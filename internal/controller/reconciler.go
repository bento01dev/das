package controller

import (
	"context"
	"fmt"

	"github.com/bento01dev/das/internal/config"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type containerDetail struct {
	sidecarConfig   config.SidecarConfig
	containerStatus corev1.ContainerStatus
}

type PodReconciler struct {
	client.Client
	conf config.Config
}

func (r *PodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	pod := &corev1.Pod{}
	err := r.Get(ctx, req.NamespacedName, pod)
	if err != nil {
		fmt.Printf("error getting pod for %s: %v", req.NamespacedName, err)
		return ctrl.Result{}, nil
	}
	// pod.Status.ContainerStatuses[0].State.Terminated
	// pod.Status.ContainerStatuses[0].State.Terminated.ContainerID
	details := r.matchDetails(pod)
	fmt.Println("matched details:", details)
	return ctrl.Result{}, nil
}

func (r *PodReconciler) matchDetails(pod *corev1.Pod) map[string]containerDetail {
	var res = make(map[string]containerDetail)
	for _, name := range r.conf.GetSidecarNames() {
		for _, containerStatus := range pod.Status.ContainerStatuses {
			if name == containerStatus.Name {
				sidecarConfig := r.conf.Sidecars[name]
				res[name] = containerDetail{sidecarConfig: sidecarConfig, containerStatus: containerStatus}
			}
		}
	}
	return res
}
