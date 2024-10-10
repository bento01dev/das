package controller

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/bento01dev/das/internal/config"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
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

type dasDetail struct {
	Name         string `json:"name"`
	RestartCount int    `json:"restart_count"`
}

type modifier interface {
	getOwnerDetails(pod *corev1.Pod) map[config.Owner]types.NamespacedName
	getCurrentStep(sidecarConfig config.SidecarConfig, stepName string) config.ResourceStep
	getNextStep(sidecarConfig config.SidecarConfig, currentStep string) int
	matchDetails(pod *corev1.Pod) []containerDetail
	filterTerminated(details []containerDetail) []containerDetail
	groupByOwner(details []containerDetail) map[config.Owner][]containerDetail
	newAnnotations(details []containerDetail, currentOwnerAnnotations map[string]string, currentPodAnnotations map[string]string) (ownerAnnotations map[string]string, podAnnotations map[string]string, err error)
}

type PodReconciler struct {
	client.Client
	conf     config.Config
	modifier modifier
}

func NewPodReconciler(c client.Client, conf config.Config, m modifier) *PodReconciler {
	return &PodReconciler{
		Client:   c,
		conf:     conf,
		modifier: m,
	}
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
// TODO: figure out operation failed. object updated error
func (r *PodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	pod := &corev1.Pod{}
	err := r.Get(ctx, req.NamespacedName, pod)
	if err != nil {
		slog.Error("error getting pod", "pod_name", req.NamespacedName.Name, "namespace", req.Namespace, "err", err.Error())
		return ctrl.Result{}, nil
	}
	ownerDetails := r.modifier.getOwnerDetails(pod)
	details := r.modifier.matchDetails(pod)
	details = r.modifier.filterTerminated(details)
	if len(details) < 1 {
		return ctrl.Result{}, nil
	}
	groupedDetails := r.modifier.groupByOwner(details)
	err = r.updateOwners(ctx, groupedDetails, ownerDetails)
	if err != nil {
		slog.Error("error in updating owner", "pod_name", req.NamespacedName.Name, "namespace", req.Namespace, "err", err.Error())
		return ctrl.Result{}, err
	}
	slog.Info("owner successfully updated", "pod_name", req.Name, "namespace", req.Namespace)
	return ctrl.Result{}, nil
}

func (r *PodReconciler) updateOwners(ctx context.Context, groupedDetails map[config.Owner][]containerDetail, ownerNamespacedNames map[config.Owner]types.NamespacedName) error {
	var err error
	for owner, details := range groupedDetails {
		switch owner {
		case config.Deployment:
			err = r.updateDeployment(ctx, details, ownerNamespacedNames)
		case config.DaemonSet:
			err = r.updateDaemonSet(ctx, details, ownerNamespacedNames)
		}
	}
	return err
}

func (r *PodReconciler) updateDeployment(ctx context.Context, details []containerDetail, ownerNamespacedNames map[config.Owner]types.NamespacedName) error {
	var err error
	replicaNamespacedName, ok := ownerNamespacedNames[config.ReplicaSet]
	if !ok {
		return errors.New("replica set detail not found in map")
	}
	var replicaSet appsv1.ReplicaSet
	err = r.Get(ctx, replicaNamespacedName, &replicaSet)
	if err != nil {
		return fmt.Errorf("error in retrieving replica set as owner of pod: %w", err)
	}
	var deploymentNamespacedName types.NamespacedName
	for _, owner := range replicaSet.OwnerReferences {
		if config.Owner(owner.Kind) == config.Deployment {
			deploymentNamespacedName = types.NamespacedName{Namespace: replicaNamespacedName.Namespace, Name: owner.Name}
			break
		}
	}
	var deployment appsv1.Deployment
	err = r.Get(ctx, deploymentNamespacedName, &deployment)
	if err != nil {
		return err
	}
	currentOwnerAnnotations := deployment.ObjectMeta.Annotations
	currentPodAnnotations := deployment.Spec.Template.Annotations
	ownerAnnotations, podAnnotations, err := r.modifier.newAnnotations(details, currentOwnerAnnotations, currentPodAnnotations)
	if err != nil {
		fmt.Println("some error in generating new annotations..")
		return fmt.Errorf("error in updating annotations for %s in %s: %w", deployment.Name, deployment.Namespace, err)
	}
	deployment.ObjectMeta.Annotations = ownerAnnotations
	deployment.Spec.Template.Annotations = podAnnotations

	err = r.Update(ctx, &deployment)
	if err != nil {
		fmt.Println("error in updating deployment:", err.Error())
		return fmt.Errorf("error updating deployment with the new annotations for %s: %w", deployment.Name, err)
	}

	return err
}

func (r *PodReconciler) updateDaemonSet(ctx context.Context, details []containerDetail, ownerDetails map[config.Owner]types.NamespacedName) error {
	var err error
	daemonSetNamespacedName, ok := ownerDetails[config.DaemonSet]
	if !ok {
		return errors.New("daemon set namespaced name not found in owner details")
	}
	var daemonSet appsv1.DaemonSet
	err = r.Get(ctx, daemonSetNamespacedName, &daemonSet)
	if err != nil {
		return fmt.Errorf("error in retrieving daemon set details for %v: %w", daemonSetNamespacedName, err)
	}
	currentOwnerAnnotations := daemonSet.ObjectMeta.Annotations
	currentPodAnnotations := daemonSet.Spec.Template.Annotations
	ownerAnnotations, podAnnotations, err := r.modifier.newAnnotations(details, currentOwnerAnnotations, currentPodAnnotations)
	if err != nil {
		return fmt.Errorf("error in updating annotations for %s in %s: %w", daemonSet.Name, daemonSet.Namespace, err)
	}
	daemonSet.ObjectMeta.Annotations = ownerAnnotations
	daemonSet.Spec.Template.Annotations = podAnnotations

	err = r.Update(ctx, &daemonSet)
	if err != nil {
		fmt.Println("err in updating daemon set:", err.Error())
		return fmt.Errorf("error updating deployment with the new annotations for %s: %w", daemonSet.Name, err)
	}

	return err
}
