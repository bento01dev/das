package controller

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/bento01dev/das/internal/blob"
	"github.com/bento01dev/das/internal/config"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

func Start(conf config.Config) error {
	manager, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		LeaderElection:          true,
		LeaderElectionID:        "das-controller",
		LeaderElectionNamespace: "das",
	})
	if err != nil {
		return fmt.Errorf("error creating new manager with cluster config: %w", err)
	}
	modifier := NewPodOwnerModifier(conf)
	storer, err := getStorer()
	if err != nil {
		return fmt.Errorf("error setting storer: %w", err)
	}

	err = ctrl.
		NewControllerManagedBy(manager).
		For(&corev1.Pod{}).
		Complete(NewPodReconciler(manager.GetClient(), conf, modifier, storer))
	if err != nil {
		return fmt.Errorf("error in setting reconciler for pod: %w", err)
	}

	slog.Info("starting manager for das..")
	return manager.Start(ctrl.SetupSignalHandler())
}

func getStorer() (storer, error) {
	storageType := os.Getenv("STORAGE_TYPE")
	slog.Info("initialising step store", "storage_type", storageType)
	switch strings.ToLower(storageType) {
	case "s3":
		slog.Info("initialising s3 store")
		return getS3Storer()
	default:
		slog.Info("initialising dummy store..")
		return blob.DummyStepStore{}, nil
	}
}

func getS3Storer() (storer, error) {
	bucketName := os.Getenv("S3_BUCKET")
	if bucketName == "" {
		return nil, errors.New("bucket name not set")
	}

	awsEndpoint := os.Getenv("AWS_ENDPOINT")
	return blob.NewS3StepStore(bucketName, awsEndpoint)
}
