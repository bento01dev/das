package blob

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/bento01dev/das/internal/config"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type S3StepStore struct {
	client     *s3.Client
	bucketName string
}

func NewS3StepStore() (S3StepStore, error) {
	var store S3StepStore
	cfg, err := awsConfig.LoadDefaultConfig(context.Background())
	if err != nil {
		return store, err
	}
	bucketName := os.Getenv("S3_BUCKET")
	if bucketName == "" {
		return store, errors.New("bucket name not set")
	}

	awsEndpoint := os.Getenv("AWS_ENDPOINT")
	if awsEndpoint != "" {
		client := s3.NewFromConfig(cfg, func(o *s3.Options) {
			o.UsePathStyle = true
			o.BaseEndpoint = aws.String(awsEndpoint)
		})
		return S3StepStore{client: client, bucketName: bucketName}, nil
	}

	client := s3.NewFromConfig(cfg)
	return S3StepStore{client: client, bucketName: bucketName}, nil
}

func (store S3StepStore) UploadNewSteps(appName string, steps map[string]config.ResourceStep) (string, error) {
	data, err := json.Marshal(steps)
	if err != nil {
		return "", err
	}
	fileName := fmt.Sprintf("%s.json", appName)
	output, err := store.client.PutObject(context.TODO(), &s3.PutObjectInput{
		Bucket: aws.String(store.bucketName),
		Key:    aws.String(fileName),
		Body:   bytes.NewReader(data),
	})
	if err != nil {
		return "", err
	}
	return *output.ETag, nil
}
