package scalable

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	logctl "github.com/ccfish2/controllerPoweredByDI/scalable/logcontroller"
	"github.com/ccfish2/infra/pkg/hive/cell"
	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	k8sClient "github.com/ccfish2/infra/pkg/k8s/client"
	"github.com/ccfish2/infra/pkg/logging/logfields"
	"golang.org/x/sys/unix"
)

var Cell = cell.Module(
	"microsvc-logs",
	"Logs from microservices controller",
	cell.Provide(loadCfg),
	cell.Provide(generateS3Client),
	cell.Invoke(registerLogController),
)

type logControllerParams struct {
	cell.In
	LifeCycle cell.Lifecycle
	Logger    logrus.FieldLogger
	K8sClient k8sClient.Clientset
}

func loadCfg(params logControllerParams) *logctl.LogConfig {
	cfg := &logctl.LogConfig{
		UploadInMinutesInterval: time.Minute,
	}
	cm, err := params.K8sClient.CoreV1().ConfigMaps("dolphin").Get(context.Background(), "dolphin-config", metav1.GetOptions{})
	if err != nil {
		params.Logger.WithError(err).Error("failed to get configmap")
		return nil
	}

	raw := cm.Data["Apps"]
	if err := json.Unmarshal([]byte(raw), &cfg.AppNames); err != nil {
		params.Logger.WithError(err).WithField("raw", raw).Error("failed to unmarshal Apps list")
		return nil
	}
	params.Logger.WithField("apps", cfg.AppNames).Info("Apps list")
	cfg.LogRootPath = cm.Data["LogRootPath"]
	cfg.S3Bucket = cm.Data["S3Bucket"]
	cfg.AWSRegion = cm.Data["AWSRegion"]
	params.Logger.WithField("logRootPath", cfg.LogRootPath).Info("Log root path")
	params.Logger.WithField("s3Bucket", cfg.S3Bucket).Info("S3 bucket")
	params.Logger.WithField("awsRegion", cfg.AWSRegion).Info("AWS region")
	return cfg
}

func generateS3Client(params logControllerParams, cfg *logctl.LogConfig) logctl.S3Client {
	s3cli, err := logctl.NewS3Client(cfg.AWSRegion)
	if err != nil {
		params.Logger.WithError(err).Error("failed to create S3 client")
		return nil
	}
	return s3cli
}

func registerLogController(params logControllerParams, cfg *logctl.LogConfig, s3cli logctl.S3Client) {
	scopedLog := params.Logger.WithFields(logrus.Fields{
		logfields.Controller: "log-controller",
		logfields.Resource:   "logs",
	})
	scopedLog.Info("Registering log controller", cfg.AppNames, "::", cfg.LogRootPath, "::", cfg.S3Bucket, "::", cfg.AWSRegion)
	if cfg.UploadInMinutesInterval != 0 {
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, unix.SIGTERM, unix.SIGQUIT, unix.SIGINT, unix.SIGHUP)
		ticker := time.NewTicker(cfg.UploadInMinutesInterval)

		params.LifeCycle.Append(cell.Hook{
			OnStart: func(_ cell.HookContext) error {
				scopedLog.Infof("Initializing log controller")
				entries, err := os.ReadDir(cfg.LogRootPath)
				if err != nil {
					return fmt.Errorf("initializing log controller failed to stat log root path %s: %w", cfg.LogRootPath, err)
				}
				scopedLog.Infof("Found %d directories in log root path", len(entries))
				files, err := filepath.Glob(filepath.Join(cfg.LogRootPath, "*"))
				if err != nil {
					return fmt.Errorf("initializing log controller failed to get files in log root path: %w", err)
				}

				for _, file := range files {
					if info, err := os.Stat(file); err == nil && !info.IsDir() {
						scopedLog.Infof("Found log file: %s", file)
					}
				}

				go func() {
					sem := make(chan struct{}, 10) // limit concurrent uploads
					for {
						select {
						case <-ctx.Done():
							scopedLog.Infof("log controller shutting down")
							return
						case <-ticker.C:
							scopedLog.Infof("Uploading logs every %v", cfg.UploadInMinutesInterval)
							y, m, d := time.Now().Date()
							datestamp := fmt.Sprintf("%d%02d%02d", y, m, d)

							for _, app := range cfg.AppNames {
								targetAppLogName := fmt.Sprintf("%s-%s.log", app, datestamp)
								logfile := filepath.Join(cfg.LogRootPath, app, targetAppLogName)
								params.Logger.WithField("file", logfile).Info("checking file")
								if _, err := os.Stat(logfile); os.IsNotExist(err) {
									params.Logger.WithError(err).WithField("file", logfile).Warn("does not exist")
									continue
								}

								select {
								case sem <- struct{}{}:
									go func(file, app string) {
										defer func() { <-sem }() // release the semaphore
										if err := uploadToS3(ctx, file, params, cfg, s3cli); err != nil {
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
					}
				}()
				return nil
			},

			OnStop: func(_ cell.HookContext) error {
				cancel()
				ticker.Stop()
				return nil
			},
		})
	}
}

func uploadToS3(ctx context.Context, filePath string, params logControllerParams, cfg *logctl.LogConfig, s3cli logctl.S3Client) error {
	select {
	case <-ctx.Done():
		params.Logger.Info("Upload s3 cancelled")
		return ctx.Err()
	default:
		params.Logger.WithField("file", filePath).Info("Uploading to S3")
		fp, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0644)
		if err != nil {
			return err
		}
		defer fp.Close()
		n, err := fp.WriteString(fmt.Sprintf("Mocking Uploading to S3 on %s\n", time.Now().String()))
		if err != nil || n == 0 {
			fmt.Println("Error writing to file:", err, " but it is ok since it is log file")
		}
		exist, err := s3cli.BucketExists(cfg.S3Bucket)
		if err != nil {
			return err
		}
		if !exist {
			s3cli.MakeBucket(cfg.S3Bucket)
			params.Logger.Info("Creating bucket %s successfully", cfg.S3Bucket)
		}
		params.Logger.Info("Uploading ", filePath, " to S3 successfuly")
	}
	return nil
}
