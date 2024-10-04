package controller

import (
	"fmt"

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

	err = ctrl.
		NewControllerManagedBy(manager).
		For(&corev1.Pod{}).
		Complete(&PodReconciler{Client: manager.GetClient(), conf: conf})
	if err != nil {
		return fmt.Errorf("error in setting reconciler for pod: %w", err)
	}

	fmt.Println("starting manager..")
	//TODO: need to pass custom context
	return manager.Start(ctrl.SetupSignalHandler())
}
