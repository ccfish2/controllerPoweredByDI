package logcontroller

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Create an IAM User for S3 Access
// aws iam create-user --user-name dolphin-operator-user
// Attach a minimal S3 policy
/*
`
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "s3:PutObject",
        "s3:GetObject",
        "s3:ListBucket"
      ],
      "Resource": [
        "arn:aws:s3:::your-bucket-name",
        "arn:aws:s3:::your-bucket-name/*"
      ]
    }
  ]
}
`
*/

/*
attach policy to the operator user
aws iam put-user-policy \
  --user-name dolphin-operator-user \
  --policy-name S3AccessPolicy \
  --policy-document file://s3-policy.json
*/
// create AWS access key
// aws iam create-access-key --user-name dolphin-operator-user

type S3Client interface {
	// KeyExists checks if object exists (and if we have permission to access)
	KeyExists(bucket, key string) (bool, error)
	IsDirectory(bucket, key string) (bool, error)
	BucketExists(bucket string) (bool, error)

	ListDirectory(bucket, keyPrefix string) ([]string, error)
	GetFile(bucket, key string) error
	GetDirectory(bucket, key, path string) error

	MakeBucket(bucketName string) error
	PutDirectory(bucket, key, path string) error
	PutFile(bucket, key, path string) error

	Delete(bucket, key string) error
}

type S3ClientOpts struct {
	AccessKey string
	SecretKey string
}

type s3client struct {
	ctx      context.Context
	s3Client *s3.Client
}

// BucketExists implements S3Client.
func (s *s3client) BucketExists(bucket string) (bool, error) {
	panic("unimplemented")
}

// Delete implements S3Client.
func (s *s3client) Delete(bucket string, key string) error {
	panic("unimplemented")
}

// GetDirectory implements S3Client.
func (s *s3client) GetDirectory(bucket string, key string, path string) error {
	panic("unimplemented")
}

// GetFile implements S3Client.
func (s *s3client) GetFile(bucket string, key string) error {
	panic("unimplemented")
}

// IsDirectory implements S3Client.
func (s *s3client) IsDirectory(bucket string, key string) (bool, error) {
	panic("unimplemented")
}

// KeyExists implements S3Client.
func (s *s3client) KeyExists(bucket string, key string) (bool, error) {
	panic("unimplemented")
}

// ListDirectory implements S3Client.
func (s *s3client) ListDirectory(bucket string, keyPrefix string) ([]string, error) {
	panic("unimplemented")
}

// MakeBucket implements S3Client.
func (s *s3client) MakeBucket(bucketName string) error {
	panic("unimplemented")
}

// PutDirectory implements S3Client.
func (s *s3client) PutDirectory(bucket string, key string, path string) error {
	panic("unimplemented")
}

// PutFile implements S3Client.
func (s *s3client) PutFile(bucket string, key string, path string) error {
	panic("unimplemented")
}

func NewS3Client(ctx context.Context, AWSRegion string) (S3Client, error) {
	s3cli := &s3client{
		ctx: ctx,
	}
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(AWSRegion))
	if err != nil {
		return nil, err
	}
	s3cli.s3Client = s3.NewFromConfig(cfg)

	return s3cli, nil
}
