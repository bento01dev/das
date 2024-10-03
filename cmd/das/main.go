package main

import (
	"context"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bento01dev/das/internal/command"
)

// just added to check if dependencies are loaded. will delete soon
var _ = aws.Int(5)
var _, _ = config.LoadDefaultConfig(context.TODO())
var _ = s3.New(s3.Options{})

func main() {
	if err := command.Run(); err != nil {
		os.Exit(1)
	}
}
