package controller

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"

	"github.com/bento01dev/das/internal/config"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

type newAnnotations struct {
	ownerAnnotations map[string]string
	podAnnotations   map[string]string
	steps            map[string]config.ResourceStep
}

type PodOwnerModifier struct {
	conf config.Config
}

func NewPodOwnerModifier(conf config.Config) PodOwnerModifier {
	return PodOwnerModifier{conf: conf}
}

func (p PodOwnerModifier) getOwnerDetails(pod *corev1.Pod) map[config.Owner]types.NamespacedName {
	res := make(map[config.Owner]types.NamespacedName)
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
		default:
			slog.Debug("unsupported owner", "owner_type", owner)
			continue
		}
		res[owner] = types.NamespacedName{Namespace: pod.Namespace, Name: ownerRef.Name}
	}
	return res
}

func (p PodOwnerModifier) getCurrentStep(sidecarConfig config.SidecarConfig, stepName string) config.ResourceStep {
	i := slices.IndexFunc(sidecarConfig.Steps, func(step config.ResourceStep) bool {
		return step.Name == stepName
	})

	if i == -1 {
		slog.Info("no step found for given name, returning empty step", "step_name", stepName)
		return config.ResourceStep{}
	}
	return sidecarConfig.Steps[i]
}

func (p PodOwnerModifier) getNextStep(sidecarConfig config.SidecarConfig, currentStep string) int {
	res := slices.IndexFunc(sidecarConfig.Steps, func(step config.ResourceStep) bool {
		if step.Name == currentStep {
			return true
		}
		return false
	})

	if res == -1 {
		slog.Info("Current step not found. returning last step to be safe..", "step_name", currentStep)
		return len(sidecarConfig.Steps) - 1
	}

	if res == len(sidecarConfig.Steps)-1 {
		slog.Info("On last step.. so returning the last step..", "step_name", currentStep)
		return len(sidecarConfig.Steps) - 1
	}

	return res + 1
}

func (p PodOwnerModifier) matchDetails(pod *corev1.Pod) []containerDetail {
	var res []containerDetail
	for name, sidecarConfig := range p.conf.Sidecars {
		for _, containerStatus := range pod.Status.ContainerStatuses {
			if name == containerStatus.Name {
				res = append(res, containerDetail{sidecarConfig: sidecarConfig, containerStatus: containerStatus})
			}
		}
	}
	return res
}

func (p PodOwnerModifier) filterTerminated(details []containerDetail) []containerDetail {
	var filtered []containerDetail
	for _, detail := range details {
		if detail.containerStatus.State.Terminated != nil &&
			slices.Contains(detail.sidecarConfig.ErrCodes, int(detail.containerStatus.State.Terminated.ExitCode)) {
			slog.Debug("container termination matches error codes", "container_name", detail.containerStatus.Name)
			filtered = append(filtered, detail)
		}
	}
	return filtered
}

func (p PodOwnerModifier) groupByOwner(details []containerDetail) map[config.Owner][]containerDetail {
	var res = make(map[config.Owner][]containerDetail)
	for _, detail := range details {
		res[detail.sidecarConfig.Owner] = append(res[detail.sidecarConfig.Owner], detail)
	}
	return res
}

func (p PodOwnerModifier) newAnnotations(details []containerDetail, currentOwnerAnnotations map[string]string, currentPodAnnotations map[string]string) (newAnnotations, error) {
	var (
		res              newAnnotations
		ownerAnnotations map[string]string
		podAnnotations   map[string]string
		steps            map[string]config.ResourceStep = make(map[string]config.ResourceStep)
		err              error
	)

	ownerAnnotations = currentOwnerAnnotations
	if ownerAnnotations == nil {
		ownerAnnotations = make(map[string]string)
	}

	podAnnotations = currentPodAnnotations
	if podAnnotations == nil {
		podAnnotations = make(map[string]string)
	}

	var dasDetails = make(map[string]dasDetail)
	dasDetailsStr, ok := currentOwnerAnnotations["das/details"]
	if !ok {
		for _, d := range details {
			dasDetails[d.containerStatus.Name] = dasDetail{Name: d.sidecarConfig.Steps[0].Name, RestartCount: 1}
		}
		marshalledDetails, marshallErr := json.Marshal(dasDetails)
		if err != nil {
			slog.Error("error marshalling json for das details", "err", marshallErr.Error())
			return res, fmt.Errorf("error marshalling json for %v: %w", dasDetails, marshallErr)
		}
		ownerAnnotations["das/details"] = string(marshalledDetails)
		res.ownerAnnotations = ownerAnnotations
		res.podAnnotations = podAnnotations
		return res, nil
	}

	if unmarshalErr := json.Unmarshal([]byte(dasDetailsStr), &dasDetails); unmarshalErr != nil {
		slog.Error("error in unmarshalling das details", "err", unmarshalErr.Error())
		err = fmt.Errorf("error parsing das details in %w", unmarshalErr)
		return res, err
	}

	for _, d := range details {
		restartDetail, ok := dasDetails[d.containerStatus.Name]
		if !ok {
			slog.Debug("no existing das detail for container. adding first step", "container_name", d.containerStatus.Name, "step_name", d.sidecarConfig.Steps[0].Name, "restart_count", 1)
			dasDetails[d.containerStatus.Name] = dasDetail{Name: d.sidecarConfig.Steps[0].Name, RestartCount: 1}
			continue
		}
		currentStep := p.getCurrentStep(d.sidecarConfig, restartDetail.Name)
		if restartDetail.RestartCount+1 < currentStep.RestartLimit {
			slog.Debug("restart count less than current step limit", "container_name", d.containerStatus.Name, "step_name", restartDetail.Name, "restart_count", restartDetail.RestartCount+1)
			dasDetails[d.containerStatus.Name] = dasDetail{Name: restartDetail.Name, RestartCount: restartDetail.RestartCount + 1}
			continue
		}
		nextStep := d.sidecarConfig.Steps[p.getNextStep(d.sidecarConfig, restartDetail.Name)]
		if currentStep.Name == nextStep.Name {
			slog.Debug("current step and next step are the same. so its in the last step. just incrementing count.", "container_name", d.containerStatus.Name, "step_name", nextStep.Name, "restart_count", restartDetail.RestartCount+1)
			dasDetails[d.containerStatus.Name] = dasDetail{Name: nextStep.Name, RestartCount: restartDetail.RestartCount + 1}
			continue
		}
		slog.Info("Setting next step as new step for das detail for container", "container_name", d.containerStatus.Name, "step_name", nextStep.Name)
		dasDetails[d.containerStatus.Name] = dasDetail{Name: nextStep.Name}
		podAnnotations[d.sidecarConfig.CPUAnnotationKey] = nextStep.CPURequest
		podAnnotations[d.sidecarConfig.CPULimitAnnotationKey] = nextStep.CPULimit
		podAnnotations[d.sidecarConfig.MemAnnotationKey] = nextStep.MemRequest
		podAnnotations[d.sidecarConfig.MemLimitAnnotationKey] = nextStep.MemLimit
		steps[d.containerStatus.Name] = nextStep
	}

	newDasDetails, marshalErr := json.Marshal(dasDetails)
	if marshalErr != nil {
		slog.Error("error in marhsalling new das details", "err", marshalErr)
		err = fmt.Errorf("Error in marshalling the new das details after determining next step: %w", marshalErr)
		return res, nil
	}

	slog.Debug("setting new das details", "das_details", string(newDasDetails))
	ownerAnnotations["das/details"] = string(newDasDetails)

	res.ownerAnnotations = ownerAnnotations
	res.podAnnotations = podAnnotations
	res.steps = steps

	return res, nil
}
