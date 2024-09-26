package controller

import (
	"context"
	"fmt"
	"slices"

	"github.com/bento01dev/das/internal/config"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type containerDetail struct {
	sidecarConfig   config.SidecarConfig
	containerStatus corev1.ContainerStatus
}

type podOwnerDetail struct {
	Namespace string
	Name      string
}

type PodReconciler struct {
	client.Client
	conf config.Config
}

// on pod event, it should first get the containers and retrieve configs matching name
// then it should check if container in terminating state and if it matches the listed error codes in config
// it should check if all containers are owned by the same resource as per config (for combining update)
// after that it should pick the owner in config for the container and work up to
// reference of the owner.
// after that it should check metadata of the owner to see if metadata has prior restart count
// if count less than restart count in config, increment count and do nothing
// if count more, then determine the next step in limits based on the current limits
// update all resources (or single resource if multiple containers are having errors and tied to the same resource)
// update should include resetting the count for the containers in meta data and updating the necessary annotation for the container
// update S3 to the new limits for the container for the resource name
func (r *PodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	pod := &corev1.Pod{}
	err := r.Get(ctx, req.NamespacedName, pod)
	if err != nil {
		fmt.Printf("error getting pod for %s: %v", req.NamespacedName, err)
		return ctrl.Result{}, nil
	}
	// pod.OwnerReferences[0].Name
	// pod.Status.ContainerStatuses[0].State.Terminated
	// pod.Status.ContainerStatuses[0].State.Terminated.ContainerID
	ownerDetails := r.getOwnerDetails(pod)
	details := r.matchDetails(pod)
	details = r.filterTerminated(details)
	groupedDetails := r.groupByOwner(details)
	err = r.updateOwners(groupedDetails, ownerDetails)
	if err != nil {
		return ctrl.Result{}, err
	}
	fmt.Println("matched details:", details)
	return ctrl.Result{}, nil
}

func (r *PodReconciler) getOwnerDetails(pod *corev1.Pod) map[config.Owner]podOwnerDetail {
	var res = make(map[config.Owner]podOwnerDetail)
	for _, ownerRef := range pod.OwnerReferences {
		var owner config.Owner
		kind := config.Owner(ownerRef.Kind)
		switch kind {
		case config.Deployment:
			owner = config.Deployment
		case config.ReplicaSet:
			owner = config.ReplicaSet
		case config.DaemonSet:
			owner = config.DaemonSet
		}
		if owner == "" {
			continue
		}
		res[owner] = podOwnerDetail{Namespace: pod.Namespace, Name: ownerRef.Name}
	}
	return res
}

func (r *PodReconciler) matchDetails(pod *corev1.Pod) []containerDetail {
	var res []containerDetail
	for _, name := range r.conf.GetSidecarNames() {
		for _, containerStatus := range pod.Status.ContainerStatuses {
			if name == containerStatus.Name {
				sidecarConfig := r.conf.Sidecars[name]
				res = append(res, containerDetail{sidecarConfig: sidecarConfig, containerStatus: containerStatus})
			}
		}
	}
	return res
}

func (r *PodReconciler) filterTerminated(details []containerDetail) []containerDetail {
	var filtered []containerDetail
	for _, detail := range details {
		if detail.containerStatus.State.Terminated != nil &&
			slices.Contains(detail.sidecarConfig.ErrCodes, int(detail.containerStatus.State.Terminated.ExitCode)) {
			filtered = append(filtered, detail)
		}
	}
	return filtered
}

func (r *PodReconciler) groupByOwner(details []containerDetail) map[config.Owner][]containerDetail {
	var res = make(map[config.Owner][]containerDetail)
	for _, detail := range details {
		mappedDetails := res[detail.sidecarConfig.Owner]
		mappedDetails = append(mappedDetails, detail)
		res[detail.sidecarConfig.Owner] = mappedDetails
	}
	return res
}

func (r *PodReconciler) updateOwners(groupedDetails map[config.Owner][]containerDetail, ownerDetails map[config.Owner]podOwnerDetail) error {
	var err error
	for owner, details := range groupedDetails {
		switch owner {
		case config.Deployment:
			err = r.updateDeployment(details, ownerDetails)
		case config.DaemonSet:
			err = r.updateDaemonSet(details, ownerDetails)
		}
	}
	return err
}

func (r *PodReconciler) updateDeployment(details []containerDetail, ownerDetails map[config.Owner]podOwnerDetail) error {
	var err error

	return err
}

func (r *PodReconciler) updateDaemonSet(details []containerDetail, ownerDetails map[config.Owner]podOwnerDetail) error {
	var err error
	return err
}
