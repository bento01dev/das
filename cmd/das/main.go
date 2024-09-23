package main

import (
	"context"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bento01dev/das/internal/command"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// just added to check if dependencies are loaded. will delete soon
var _ = ctrl.Options{}
var _ = corev1.Pod{}
var _ = client.CreateOptions{}
var _ = aws.Int(5)
var _, _ = config.LoadDefaultConfig(context.TODO())
var _ = s3.New(s3.Options{})

func main() {
	if err := command.Run(); err != nil {
		os.Exit(1)
	}
}
