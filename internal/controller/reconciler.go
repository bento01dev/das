package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

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
	err = r.updateOwners(ctx, groupedDetails, ownerDetails)
	if err != nil {
		return ctrl.Result{}, err
	}
	fmt.Println("matched details:", details)
	return ctrl.Result{}, nil
}

func (r *PodReconciler) getOwnerDetails(pod *corev1.Pod) map[config.Owner]types.NamespacedName {
	var res = make(map[config.Owner]types.NamespacedName)
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
		res[owner] = types.NamespacedName{Namespace: pod.Namespace, Name: ownerRef.Name}
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
	ownerAnnotations, podAnnotations, err := newAnnotations(details, currentOwnerAnnotations, currentPodAnnotations)
	if err != nil {
        return fmt.Errorf("error in updating annotations for %s in %s: %w", deployment.Name, deployment.Namespace, err)
	}
	deployment.ObjectMeta.Annotations = ownerAnnotations
	deployment.Spec.Template.Annotations = podAnnotations

	err = r.Update(ctx, &deployment)
	if err != nil {
		return fmt.Errorf("error updating deployment with the new annotations for %s: %w", deployment.Name, err)
	}

	return err
}

func newAnnotations(details []containerDetail, currentOwnerAnnotations map[string]string, currentPodAnnotations map[string]string) (ownerAnnotations map[string]string, podAnnotations map[string]string, err error) {
	ownerAnnotations = currentOwnerAnnotations
	podAnnotations = currentPodAnnotations
	var dasDetails map[string]dasDetail
	var marshalledDetails []byte
	dasDetailsStr, ok := currentOwnerAnnotations["das/details"]
	if !ok {
		for _, d := range details {
			dasDetails[d.containerStatus.Name] = dasDetail{Name: d.sidecarConfig.Steps[0].Name, RestartCount: 1}
		}
		marshalledDetails, err = json.Marshal(dasDetails)
		if err != nil {
			err = fmt.Errorf("error marshalling json for %v: %w", dasDetails, err)
			return
		}
		ownerAnnotations["das/details"] = string(marshalledDetails)
		return
	}

	if unmarshalErr := json.Unmarshal([]byte(dasDetailsStr), &dasDetails); unmarshalErr != nil {
		err = fmt.Errorf("error parsing das details in %w", unmarshalErr)
		return
	}
	//compare das deatils with container details to check which ones are above restart count.
	//for the containers that have exceeded restart count, get the current resource limits value and
	//determine the next step value. update restart count to 0 for these containers as well
	//update deployment yaml
	//update s3 bucket.
	for _, d := range details {
		restartDetail, ok := dasDetails[d.containerStatus.Name]
		if !ok {
			dasDetails[d.containerStatus.Name] = dasDetail{Name: d.sidecarConfig.Steps[0].Name, RestartCount: 1}
			continue
		}
		currentStep := getCurrentStep(d.sidecarConfig, restartDetail.Name)
		if restartDetail.RestartCount+1 < currentStep.RestartLimit {
			dasDetails[d.containerStatus.Name] = dasDetail{Name: restartDetail.Name, RestartCount: restartDetail.RestartCount + 1}
			continue
		}
		nextStep := d.sidecarConfig.Steps[getNextStep(d.sidecarConfig, restartDetail.Name)]
		dasDetails[d.containerStatus.Name] = dasDetail{Name: nextStep.Name}
		podAnnotations[d.sidecarConfig.CPUAnnotationKey] = currentStep.CPURequest
		podAnnotations[d.sidecarConfig.CPULimitAnnotationKey] = currentStep.CPULimit
		podAnnotations[d.sidecarConfig.MemAnnotationKey] = currentStep.MemRequest
		podAnnotations[d.sidecarConfig.MemLimitAnnotationKey] = currentStep.MemLimit
	}

	newDasDetails, marshalErr := json.Marshal(dasDetails)
	if marshalErr != nil {
		err = fmt.Errorf("Error in marshalling the new das details after determining next step: %w", marshalErr)
		return
	}

	ownerAnnotations["das/details"] = string(newDasDetails)
	return
}

func getCurrentStep(sidecarConfig config.SidecarConfig, stepName string) config.ResourceStep {
	var res config.ResourceStep
	for _, step := range sidecarConfig.Steps {
		if step.Name == stepName {
			res = step
			break
		}
	}
	return res
}

func getNextStep(sidecarConfig config.SidecarConfig, currentStep string) int {
	var res = len(sidecarConfig.Steps)
	for i, step := range sidecarConfig.Steps {
		if step.Name == currentStep && i != len(sidecarConfig.Steps) {
			res = i + 1
			break
		}
	}
	return res
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
	ownerAnnotations, podAnnotations, err := newAnnotations(details, currentOwnerAnnotations, currentPodAnnotations)
	if err != nil {
        return fmt.Errorf("error in updating annotations for %s in %s: %w", daemonSet.Name, daemonSet.Namespace, err)
	}
	daemonSet.ObjectMeta.Annotations = ownerAnnotations
	daemonSet.Spec.Template.Annotations = podAnnotations

	err = r.Update(ctx, &daemonSet)
	if err != nil {
		return fmt.Errorf("error updating deployment with the new annotations for %s: %w", daemonSet.Name, err)
	}

	return err
}
