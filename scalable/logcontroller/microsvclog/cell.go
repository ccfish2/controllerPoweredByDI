package scalable

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	logctl "github.com/ccfish2/controllerPoweredByDI/scalable/logcontroller"
	"github.com/ccfish2/infra/pkg/hive/cell"
	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	k8sClient "github.com/ccfish2/infra/pkg/k8s/client"
)

var Cell = cell.Module(
	"microsvc-logs",
	"Logs from microservices controller",

	cell.Config(
		logctl.LogConfig{
			AppNames: []string{},
		},
	),
	cell.Invoke(loadCfg),
	cell.Provide(generateS3Client),
	cell.Invoke(registerLogController),
)

type logControllerParams struct {
	cell.In

	Logger    logrus.FieldLogger
	K8sClient k8sClient.Clientset
	Config    logctl.LogConfig
	S3Client  logctl.S3Client
}

func generateS3Client(cfg logctl.LogConfig) (context.Context, logctl.S3Client, error) {
	ctx := context.Background()
	s3cli, err := logctl.NewS3Client(context.Background(), cfg.AWSRegion)
	if err != nil {
		return ctx, nil, fmt.Errorf("failed to create S3 client: %w", err)
	}
	return ctx, s3cli, nil
}

func loadCfg(params logControllerParams) (logctl.LogConfig, error) {
	cfg := logctl.LogConfig{}
	cm, err := params.K8sClient.CoreV1().ConfigMaps("dolphin").Get(context.Background(), "dolphin-config", metav1.GetOptions{})
	if err != nil {
		return cfg, fmt.Errorf("failed to get configmap: %w", err)
	}

	raw := cm.Data["Apps"]
	if err := json.Unmarshal([]byte(raw), &cfg.AppNames); err != nil {
		return cfg, fmt.Errorf("failed to unmarshal Apps list: %w", err)
	}

	cfg.LogRootPath = cm.Data["LogRootPath"]
	cfg.S3Bucket = cm.Data["S3Bucket"]
	cfg.AWSRegion = cm.Data["AWSRegion"]

	return cfg, nil
}

func registerLogController(ctx context.Context, params logControllerParams) {
	ticker := time.NewTicker(12 * time.Hour)
	defer ticker.Stop()

	go func() {

		sem := make(chan struct{}, 10) // limit concurrent uploads
		for {
			select {
			case <-ctx.Done():
				fmt.Printf("log controller shutting down")
				return
			case <-ticker.C:
				uploadLogs(ctx, params, sem)
			}
		}
	}()
}

func uploadLogs(ctx context.Context, params logControllerParams, sem chan struct{}) {
	y, m, d := time.Now().Date()
	datestamp := fmt.Sprintf("%d%02d%02d", y, m, d)

	for _, app := range params.Config.AppNames {
		targetAppLogName := fmt.Sprintf("%s-%s.log", app, datestamp)
		logfile := filepath.Join(params.Config.LogRootPath, app, targetAppLogName)

		if _, err := os.Stat(logfile); os.IsNotExist(err) {
			fmt.Printf("log file does not exist")
			continue
		}

		select {
		case sem <- struct{}{}:
			go func(file, app string) {
				defer func() { <-sem }()
				if err := uploadToS3(ctx, file, params); err != nil {
					params.Logger.WithError(err).WithField("file", file).Error("upload failed")
				} else {
					params.Logger.WithField("file", file).Info("upload succeeded")
				}

				// Future Kafka push placeholder:
				// err := publishToKafka(fileMetadata(app, file))
			}(logfile, app)
		default:
			params.Logger.Warn("upload queue is full, skipping file: ", logfile)
		}
	}
}

func uploadToS3(ctx context.Context, filePath string, params logControllerParams) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		exist, err := params.S3Client.BucketExists(params.Config.S3Bucket)
		if err != nil {
			return err
		}
		if !exist {
			params.S3Client.MakeBucket(params.Config.S3Bucket)
			fmt.Printf("Creating bucket %s successfully", params.Config.S3Bucket)
		}
		fmt.Printf("Uploading %s to S3 successfuly", filePath)
	}
	return nil
}
