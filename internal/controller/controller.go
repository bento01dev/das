package controller

import (
	"fmt"
	"log/slog"

	"github.com/bento01dev/das/internal/config"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// TODO: figure out log.SetLogger error
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

	err = ctrl.
		NewControllerManagedBy(manager).
		For(&corev1.Pod{}).
		Complete(NewPodReconciler(manager.GetClient(), conf, modifier))
	if err != nil {
		return fmt.Errorf("error in setting reconciler for pod: %w", err)
	}

	slog.Info("starting manager for das..")
	//TODO: need to pass custom context
	return manager.Start(ctrl.SetupSignalHandler())
}
