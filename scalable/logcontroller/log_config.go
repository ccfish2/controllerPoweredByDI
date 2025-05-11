package logcontroller

import (
	"time"

	"github.com/spf13/pflag"
)

/*
coniguration of logs metadata
*/
type LogConfig struct {
	LogRootPath string
	AppNames    []string
	S3Bucket    string
	AWSRegion   string

	UploadInMinutesInterval time.Duration
}

func (r *LogConfig) Flags(flags *pflag.FlagSet) {
	flags.StringVar(&r.LogRootPath, "log-root-path", "", "root path of logs")
	flags.StringSliceVar(&r.AppNames, "app-names", []string{}, "names of apps to collect logs")
	flags.StringVar(&r.S3Bucket, "s3-bucket", "", "s3 bucket to upload logs")
	flags.StringVar(&r.AWSRegion, "aws-region", "", "aws region of s3 bucket")
	flags.DurationVar(&r.UploadInMinutesInterval, "upload-in-minutes-interval", time.Minute, "log monitor interval in minutes")
}
