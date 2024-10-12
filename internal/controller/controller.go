package controller

import (
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
	//TODO: need to pass custom context
	return manager.Start(ctrl.SetupSignalHandler())
}

func getStorer() (storer, error) {
	storageType := os.Getenv("STORAGE_TYPE")
	slog.Info("initialising step store", "storage_type", storageType)
	switch strings.ToLower(storageType) {
	case "s3":
		return blob.NewS3StepStore()
	default:
		return blob.DummyStepStore{}, nil
	}
}
