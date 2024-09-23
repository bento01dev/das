package controller

import (
	"context"

	"github.com/bento01dev/das/internal/config"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type PodReconciler struct {
	client.Client
	conf config.Config
}

func (r *PodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return ctrl.Result{}, nil
}
