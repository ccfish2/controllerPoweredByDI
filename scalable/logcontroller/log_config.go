package logcontroller

import "github.com/spf13/pflag"

/*
coniguration of logs metadata
*/
type LogConfig struct {
	LogRootPath string
	AppNames    []string
	S3Bucket    string
	AWSRegion   string
}

func (r LogConfig) Flags(flags *pflag.FlagSet) {
	flags.String("log-root-path", r.LogRootPath, "root path of logs")
	flags.StringSlice("app-names", r.AppNames, "names of apps to collect logs")
	flags.String("s3-bucket", r.S3Bucket, "s3 bucket to upload logs")
}
