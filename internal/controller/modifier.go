package controller

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/bento01dev/das/internal/config"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

type PodOwnerModifier struct {
	conf config.Config
}

func NewPodOwnerModifier(conf config.Config) PodOwnerModifier {
	return PodOwnerModifier{conf: conf}
}

func (p PodOwnerModifier) getOwnerDetails(pod *corev1.Pod) map[config.Owner]types.NamespacedName {
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

func (p PodOwnerModifier) getCurrentStep(sidecarConfig config.SidecarConfig, stepName string) config.ResourceStep {
	i := slices.IndexFunc(sidecarConfig.Steps, func(step config.ResourceStep) bool {
		if step.Name == stepName {
			return true
		}
		return false
	})

	if i == -1 {
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

	if res == -1 || res == len(sidecarConfig.Steps)-1 {
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
			filtered = append(filtered, detail)
		}
	}
	return filtered
}

func (p PodOwnerModifier) groupByOwner(details []containerDetail) map[config.Owner][]containerDetail {
	var res = make(map[config.Owner][]containerDetail)
	for _, detail := range details {
		mappedDetails := res[detail.sidecarConfig.Owner]
		mappedDetails = append(mappedDetails, detail)
		res[detail.sidecarConfig.Owner] = mappedDetails
	}
	return res
}

func (p PodOwnerModifier) newAnnotations(details []containerDetail, currentOwnerAnnotations map[string]string, currentPodAnnotations map[string]string) (ownerAnnotations map[string]string, podAnnotations map[string]string, err error) {
	if currentOwnerAnnotations != nil {
		ownerAnnotations = currentOwnerAnnotations
	} else {
		ownerAnnotations = make(map[string]string)
	}

	if currentPodAnnotations != nil {
		podAnnotations = currentPodAnnotations
	} else {
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
			err = fmt.Errorf("error marshalling json for %v: %w", dasDetails, marshallErr)
			return
		}
		ownerAnnotations["das/details"] = string(marshalledDetails)
		return
	}

	if unmarshalErr := json.Unmarshal([]byte(dasDetailsStr), &dasDetails); unmarshalErr != nil {
		fmt.Println("error in unmarshalling:", unmarshalErr.Error())
		err = fmt.Errorf("error parsing das details in %w", unmarshalErr)
		return
	}

	for _, d := range details {
		restartDetail, ok := dasDetails[d.containerStatus.Name]
		if !ok {
			dasDetails[d.containerStatus.Name] = dasDetail{Name: d.sidecarConfig.Steps[0].Name, RestartCount: 1}
			continue
		}
		currentStep := p.getCurrentStep(d.sidecarConfig, restartDetail.Name)
		if restartDetail.RestartCount+1 < currentStep.RestartLimit {
			dasDetails[d.containerStatus.Name] = dasDetail{Name: restartDetail.Name, RestartCount: restartDetail.RestartCount + 1}
			continue
		}
		nextStep := d.sidecarConfig.Steps[p.getNextStep(d.sidecarConfig, restartDetail.Name)]
		if currentStep.Name == nextStep.Name {
			dasDetails[d.containerStatus.Name] = dasDetail{Name: nextStep.Name, RestartCount: restartDetail.RestartCount + 1}
			continue
		}
		dasDetails[d.containerStatus.Name] = dasDetail{Name: nextStep.Name}
		podAnnotations[d.sidecarConfig.CPUAnnotationKey] = nextStep.CPURequest
		podAnnotations[d.sidecarConfig.CPULimitAnnotationKey] = nextStep.CPULimit
		podAnnotations[d.sidecarConfig.MemAnnotationKey] = nextStep.MemRequest
		podAnnotations[d.sidecarConfig.MemLimitAnnotationKey] = nextStep.MemLimit
	}

	newDasDetails, marshalErr := json.Marshal(dasDetails)
	if marshalErr != nil {
		fmt.Println("error in marshalling:", marshalErr.Error())
		err = fmt.Errorf("Error in marshalling the new das details after determining next step: %w", marshalErr)
		return
	}

	ownerAnnotations["das/details"] = string(newDasDetails)
	return
}
