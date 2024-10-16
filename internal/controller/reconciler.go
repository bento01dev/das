package controller

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/bento01dev/das/internal/blob"
	"github.com/bento01dev/das/internal/config"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const labelName string = "app.kubernetes.io/name"

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

type updateResult struct {
	appName string
	steps   map[string]config.ResourceStep
}

type modifier interface {
	getOwnerDetails(pod *corev1.Pod) map[config.Owner]types.NamespacedName
	getCurrentStep(sidecarConfig config.SidecarConfig, stepName string) config.ResourceStep
	getNextStep(sidecarConfig config.SidecarConfig, currentStep string) int
	matchDetails(pod *corev1.Pod) []containerDetail
	filterTerminated(details []containerDetail) []containerDetail
	groupByOwner(details []containerDetail) map[config.Owner][]containerDetail
	newAnnotations(details []containerDetail, currentOwnerAnnotations map[string]string, currentPodAnnotations map[string]string) (newAnnotations, error)
}

type storer interface {
	UploadNewSteps(appName string, steps map[string]config.ResourceStep) (string, error)
}

type PodReconciler struct {
	client.Client
	conf     config.Config
	modifier modifier
	storer   storer
}

func NewPodReconciler(c client.Client, conf config.Config, m modifier, s storer) *PodReconciler {
	return &PodReconciler{
		Client:   c,
		conf:     conf,
		modifier: m,
		storer:   s,
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
func (r *PodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	pod := &corev1.Pod{}
	err := r.Get(ctx, req.NamespacedName, pod)
	if err != nil {
		slog.Error("error getting pod", "pod_name", req.NamespacedName.Name, "namespace", req.Namespace, "err", err.Error())
		// this could be because pod has been deleted. drop the event.
		// if the error code for the pods persists, it would be caught later
		return ctrl.Result{}, nil
	}

	ownerDetails := r.modifier.getOwnerDetails(pod)
	details := r.modifier.matchDetails(pod)
	details = r.modifier.filterTerminated(details)
	if len(details) < 1 {
		return ctrl.Result{}, nil
	}
	groupedDetails := r.modifier.groupByOwner(details)

	updateResult, err := r.updateOwners(ctx, groupedDetails, ownerDetails)
	if err != nil {
		slog.Error("error in updating owner", "pod_name", req.NamespacedName.Name, "namespace", req.Namespace, "err", err.Error())
		// this error could be because of conflict in update. retry with backoff as normal
		return ctrl.Result{}, err
	}

	if updateResult.steps != nil && len(updateResult.steps) > 0 {
		eTag, err := r.storer.UploadNewSteps(updateResult.appName, updateResult.steps)
		if err != nil {
			// retry if context timed out with backoff
			if errors.Is(err, blob.ErrStoreContextTimeout) {
				return ctrl.Result{}, err
			}
			// while this error can cause issues with deployment later,
			// retrying would be stress on apiserver. better to have a terminal error
			// and have alerts with metrics
			return ctrl.Result{}, reconcile.TerminalError(err)
		}
		slog.Info("new steps successfully updated", "etag", eTag)
	}

	slog.Info("owner successfully updated", "pod_name", req.Name, "namespace", req.Namespace)
	return ctrl.Result{}, nil
}

func (r *PodReconciler) updateOwners(ctx context.Context, groupedDetails map[config.Owner][]containerDetail, ownerNamespacedNames map[config.Owner]types.NamespacedName) (updateResult, error) {
	// okay, yes this for loop looks a bit weird.
	// map of owners cannot have deployment and daemon set for the same container.
	// the reason its this way is because in the case of a deployment, a mutating webhook or manual addition of annotation can happen at a deployment, replicaset or pod level.
	// for a daemonset, it can happen at a daemonset or pod level.
	// for a stateful set, it can happen at a statefulset or pod level.
	// basically a pod can be composed into different levels. the container resource limits could be set at any level above and for different containers in a pod, each respective owner needs to get updated.
	// so how does this looping then ensure groupedDetails does not show up with a container having say deployment and daemonset as owner?
	// boils down to the configuration. the configuration has sidecars as a map[string]SidecarConfig. SidecarConfig has Owner (not as a slice). So if you try to name the same sidecar as being owned by two different owners, config load will fail and this controller wont start.
	// i havent found a better method yet. can change the config format later.
	deploymentDetails, deploymentExists := groupedDetails[config.Deployment]
	daemonSetDetails, daemonsetExists := groupedDetails[config.DaemonSet]

	if deploymentExists && daemonsetExists {
		return updateResult{}, errors.New("deployment and daemonset cannot be owner for the same pod/container")
	}

	if deploymentExists {
		slog.Debug("calling update deployment", "details", deploymentDetails, "owner_namespaces", ownerNamespacedNames)
		deploymentRes, err := r.updateDeployment(ctx, deploymentDetails, ownerNamespacedNames)
		if err != nil {
			return updateResult{}, err
		}
		return deploymentRes, nil
	}

	if daemonsetExists {
		slog.Debug("calling update daemon set", "details", daemonSetDetails, "owner_namespaces", ownerNamespacedNames)
		daemonsetRes, err := r.updateDaemonSet(ctx, daemonSetDetails, ownerNamespacedNames)
		if err != nil {
			return updateResult{}, err
		}
		return daemonsetRes, nil
	}
	return updateResult{}, nil
}

func (r *PodReconciler) updateDeployment(ctx context.Context, details []containerDetail, ownerNamespacedNames map[config.Owner]types.NamespacedName) (updateResult, error) {
	var err error
	var res updateResult

	replicaNamespacedName, ok := ownerNamespacedNames[config.ReplicaSet]
	if !ok {
		slog.Warn("replica set detail not found in map", "owner_namespaces", ownerNamespacedNames)
		return res, errors.New("replica set detail not found in map")
	}
	var replicaSet appsv1.ReplicaSet
	err = r.Get(ctx, replicaNamespacedName, &replicaSet)
	if err != nil {
		slog.Error("failed retrieving replica set", "err", err.Error(), "owner_name", replicaNamespacedName.Name, "owner_namespace", replicaNamespacedName.Namespace)
		return res, fmt.Errorf("failed in retrieving replica set as owner of pod: %w", err)
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
		slog.Error("failed retrieving deployment", "err", err.Error(), "owner_name", deploymentNamespacedName.Name, "owner_namespace", deploymentNamespacedName.Namespace)
		return res, fmt.Errorf("failed in retrieving deployment as owner of pod: %w", err)
	}

	l := labelName
	if r.conf.LabelName != "" {
		l = r.conf.LabelName
	}
	appName := deployment.Labels[l]

	currentOwnerAnnotations := deployment.ObjectMeta.Annotations
	currentPodAnnotations := deployment.Spec.Template.Annotations

	newAnnotations, err := r.modifier.newAnnotations(details, currentOwnerAnnotations, currentPodAnnotations)
	if err != nil {
		slog.Error("failed in generating new annotations for deployment", "err", err.Error(), "current_owner_annotations", currentOwnerAnnotations, "current_pod_annotations", currentPodAnnotations)
		return res, fmt.Errorf("failed in updating annotations for %s in %s: %w", deployment.Name, deployment.Namespace, err)
	}
	deployment.ObjectMeta.Annotations = newAnnotations.ownerAnnotations
	deployment.Spec.Template.Annotations = newAnnotations.podAnnotations

	err = r.Update(ctx, &deployment)
	if err != nil {
		slog.Error("failed in updating deployment", "err", err.Error(), "owner_name", deploymentNamespacedName.Name, "owner_namespace", deploymentNamespacedName.Namespace)
		return res, fmt.Errorf("failed updating deployment with the new annotations for %s: %w", deployment.Name, err)
	}

	res = updateResult{appName: appName, steps: newAnnotations.steps}

	return res, nil
}

func (r *PodReconciler) updateDaemonSet(ctx context.Context, details []containerDetail, ownerNamespacedNames map[config.Owner]types.NamespacedName) (updateResult, error) {
	var err error
	var res updateResult

	daemonSetNamespacedName, ok := ownerNamespacedNames[config.DaemonSet]
	if !ok {
		slog.Warn("replica set detail not found in map", "owner_namespaces", ownerNamespacedNames)
		return res, errors.New("daemon set namespaced name not found in owner details")
	}
	var daemonSet appsv1.DaemonSet
	err = r.Get(ctx, daemonSetNamespacedName, &daemonSet)
	if err != nil {
		slog.Error("error retrieving replica set", "err", err.Error(), "owner_name", daemonSetNamespacedName.Name, "owner_namespace", daemonSetNamespacedName.Namespace)
		return res, fmt.Errorf("error in retrieving daemon set details for %v: %w", daemonSetNamespacedName, err)
	}
	labelName := "app.kubernetes.io/name"
	if r.conf.LabelName != "" {
		labelName = r.conf.LabelName
	}
	appName := daemonSet.Labels[labelName]
	currentOwnerAnnotations := daemonSet.ObjectMeta.Annotations
	currentPodAnnotations := daemonSet.Spec.Template.Annotations
	newAnnotations, err := r.modifier.newAnnotations(details, currentOwnerAnnotations, currentPodAnnotations)
	if err != nil {
		slog.Error("error in generating new annotations for daemonset", "err", err.Error(), "current_owner_annotations", currentOwnerAnnotations, "current_pod_annotations", currentPodAnnotations)
		return res, fmt.Errorf("error in updating annotations for %s in %s: %w", daemonSet.Name, daemonSet.Namespace, err)
	}
	daemonSet.ObjectMeta.Annotations = newAnnotations.ownerAnnotations
	daemonSet.Spec.Template.Annotations = newAnnotations.podAnnotations

	err = r.Update(ctx, &daemonSet)
	if err != nil {
		slog.Error("error in updating daemon set", "err", err.Error(), "owner_name", daemonSetNamespacedName.Name, "owner_namespace", daemonSetNamespacedName.Namespace)
		return res, fmt.Errorf("error updating deployment with the new annotations for %s: %w", daemonSet.Name, err)
	}

	res = updateResult{appName: appName, steps: newAnnotations.steps}

	return res, nil
}
