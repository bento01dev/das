package controller

import (
	"testing"

	"github.com/bento01dev/das/internal/config"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestGetOwnerDetails(t *testing.T) {
	testcases := []struct {
		name     string
		pod      *corev1.Pod
		expected map[config.Owner]types.NamespacedName
	}{
		{
			name: "owner reference of type deployment",
			pod: &corev1.Pod{
				ObjectMeta: v1.ObjectMeta{
					Namespace: "test",
					OwnerReferences: []v1.OwnerReference{
						{
							Name: "test-deployment",
							Kind: "Deployment",
						},
					},
				},
			},
			expected: map[config.Owner]types.NamespacedName{
				config.Deployment: types.NamespacedName{Namespace: "test", Name: "test-deployment"},
			},
		},
		{
			name: "owner reference of type daemonset",
			pod: &corev1.Pod{
				ObjectMeta: v1.ObjectMeta{
					Namespace: "test",
					OwnerReferences: []v1.OwnerReference{
						{
							Name: "test-daemonset",
							Kind: "DaemonSet",
						},
					},
				},
			},
			expected: map[config.Owner]types.NamespacedName{
				config.DaemonSet: types.NamespacedName{Namespace: "test", Name: "test-daemonset"},
			},
		},
		{
			name: "owner reference of type replicaset",
			pod: &corev1.Pod{
				ObjectMeta: v1.ObjectMeta{
					Namespace: "test",
					OwnerReferences: []v1.OwnerReference{
						{
							Name: "test-replicaset",
							Kind: "ReplicaSet",
						},
					},
				},
			},
			expected: map[config.Owner]types.NamespacedName{
				config.ReplicaSet: types.NamespacedName{Namespace: "test", Name: "test-replicaset"},
			},
		},
		{
			name: "unlisted owner reference means returns an empty map",
			pod: &corev1.Pod{
				ObjectMeta: v1.ObjectMeta{
					Namespace: "test",
					OwnerReferences: []v1.OwnerReference{
						{
							Name: "test-statefulset",
							Kind: "StatefulSet",
						},
					},
				},
			},
			expected: make(map[config.Owner]types.NamespacedName),
		},
		{
			name: "with multiple owners, returns only the valid ones",
			pod: &corev1.Pod{
				ObjectMeta: v1.ObjectMeta{
					Namespace: "test",
					OwnerReferences: []v1.OwnerReference{
						{
							Name: "test-statefulset",
							Kind: "StatefulSet",
						},
						{
							Name: "test-replicaset",
							Kind: "ReplicaSet",
						},
					},
				},
			},
			expected: map[config.Owner]types.NamespacedName{
				config.ReplicaSet: types.NamespacedName{Namespace: "test", Name: "test-replicaset"},
			},
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			m := NewPodOwnerModifier(config.Config{})
			res := m.getOwnerDetails(testcase.pod)
			assert.Equal(t, testcase.expected, res)
		})
	}
}

func TestGetCurrentStep(t *testing.T) {
	testcases := []struct {
		name          string
		sidecarConfig config.SidecarConfig
		stepName      string
		expected      config.ResourceStep
	}{
		{
			name: "get current step if existing in steps",
			sidecarConfig: config.SidecarConfig{
				Steps: []config.ResourceStep{
					{
						Name: "test-step",
					},
				},
			},
			stepName: "test-step",
			expected: config.ResourceStep{Name: "test-step"},
		},
		{
			name: "return empty step if step name doesnt match",
			sidecarConfig: config.SidecarConfig{
				Steps: []config.ResourceStep{},
			},
			stepName: "test-step",
			expected: config.ResourceStep{},
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			m := NewPodOwnerModifier(config.Config{})
			res := m.getCurrentStep(testcase.sidecarConfig, testcase.stepName)
			assert.Equal(t, testcase.expected, res)
		})
	}
}

func TestGetNextStep(t *testing.T) {
	testcases := []struct {
		name          string
		sidecarConfig config.SidecarConfig
		currentStep   string
		expected      int
	}{
		{
			name: "get next step when available",
			sidecarConfig: config.SidecarConfig{
				Steps: []config.ResourceStep{
					{
						Name: "test-step-1",
					},
					{
						Name: "test-step-2",
					},
				},
			},
			currentStep: "test-step-1",
			expected:    1,
		},
		{
			name: "return the last step if cannot find the current step",
			sidecarConfig: config.SidecarConfig{
				Steps: []config.ResourceStep{
					{
						Name: "test-step-1",
					},
					{
						Name: "test-step-2",
					},
				},
			},
			currentStep: "test-step-3",
			expected:    1,
		},
		{
			name: "return the last step if it is the current step",
			sidecarConfig: config.SidecarConfig{
				Steps: []config.ResourceStep{
					{
						Name: "test-step-1",
					},
					{
						Name: "test-step-2",
					},
				},
			},
			currentStep: "test-step-2",
			expected:    1,
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			m := NewPodOwnerModifier(config.Config{})
			res := m.getNextStep(testcase.sidecarConfig, testcase.currentStep)
			assert.Equal(t, testcase.expected, res)
		})
	}
}

func TestMatchDetails(t *testing.T) {
	testcases := []struct {
		name     string
		pod      *corev1.Pod
		conf     config.Config
		expected []containerDetail
	}{
		{
			name: "return empty list when no sidecar listed in conf",
		},
		{
			name: "return containers that match list in conf",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{Name: "test-container"},
					},
				},
			},
			conf: config.Config{
				Sidecars: map[string]config.SidecarConfig{
					"test-container": config.SidecarConfig{ErrCodes: []int{1, 2}},
				},
			},
			expected: []containerDetail{
				{
					sidecarConfig: config.SidecarConfig{ErrCodes: []int{1, 2}},
					containerStatus: corev1.ContainerStatus{
						Name: "test-container",
					},
				},
			},
		},
		{
			name: "returns only containers in conf and not the rest",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{Name: "test-container"},
						{Name: "test-container-1"},
					},
				},
			},
			conf: config.Config{
				Sidecars: map[string]config.SidecarConfig{
					"test-container": config.SidecarConfig{ErrCodes: []int{1, 2}},
				},
			},
			expected: []containerDetail{
				{
					sidecarConfig: config.SidecarConfig{ErrCodes: []int{1, 2}},
					containerStatus: corev1.ContainerStatus{
						Name: "test-container",
					},
				},
			},
		},
		{
			name: "does not return containers not present in pod but listed in conf",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{Name: "test-container"},
						{Name: "test-container-1"},
					},
				},
			},
			conf: config.Config{
				Sidecars: map[string]config.SidecarConfig{
					"test-container":   config.SidecarConfig{ErrCodes: []int{1, 2}},
					"test-container-2": config.SidecarConfig{ErrCodes: []int{1, 2}},
				},
			},
			expected: []containerDetail{
				{
					sidecarConfig: config.SidecarConfig{ErrCodes: []int{1, 2}},
					containerStatus: corev1.ContainerStatus{
						Name: "test-container",
					},
				},
			},
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			m := NewPodOwnerModifier(testcase.conf)
			res := m.matchDetails(testcase.pod)
			assert.Equal(t, testcase.expected, res)
		})
	}
}

func TestFilterTerminated(t *testing.T) {
	testcases := []struct {
		name     string
		details  []containerDetail
		expected []containerDetail
	}{
		{
			name: "return empty list when no container is in terminated state",
			details: []containerDetail{
				{
					containerStatus: corev1.ContainerStatus{},
				},
			},
		},
		{
			name: "return list of terminated containers with matching error codes",
			details: []containerDetail{
				{
					containerStatus: corev1.ContainerStatus{
						State: corev1.ContainerState{
							Terminated: &corev1.ContainerStateTerminated{
								ExitCode: 137,
							},
						},
					},
					sidecarConfig: config.SidecarConfig{ErrCodes: []int{137}},
				},
			},
			expected: []containerDetail{
				{
					containerStatus: corev1.ContainerStatus{
						State: corev1.ContainerState{
							Terminated: &corev1.ContainerStateTerminated{
								ExitCode: 137,
							},
						},
					},
					sidecarConfig: config.SidecarConfig{ErrCodes: []int{137}},
				},
			},
		},
		{
			name: "return empty list if terminated but not in the error codes list",
			details: []containerDetail{
				{
					containerStatus: corev1.ContainerStatus{
						State: corev1.ContainerState{
							Terminated: &corev1.ContainerStateTerminated{
								ExitCode: 137,
							},
						},
					},
					sidecarConfig: config.SidecarConfig{ErrCodes: []int{1}},
				},
			},
		},
		{
			name: "return only those that match filtering logic and skip the rest",
			details: []containerDetail{
				{
					containerStatus: corev1.ContainerStatus{
						State: corev1.ContainerState{
							Terminated: &corev1.ContainerStateTerminated{
								ExitCode: 137,
							},
						},
					},
					sidecarConfig: config.SidecarConfig{ErrCodes: []int{1}},
				},
				{
					containerStatus: corev1.ContainerStatus{
						State: corev1.ContainerState{
							Terminated: &corev1.ContainerStateTerminated{
								ExitCode: 137,
							},
						},
					},
					sidecarConfig: config.SidecarConfig{ErrCodes: []int{137}},
				},
			},
			expected: []containerDetail{
				{
					containerStatus: corev1.ContainerStatus{
						State: corev1.ContainerState{
							Terminated: &corev1.ContainerStateTerminated{
								ExitCode: 137,
							},
						},
					},
					sidecarConfig: config.SidecarConfig{ErrCodes: []int{137}},
				},
			},
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			m := NewPodOwnerModifier(config.Config{})
			res := m.filterTerminated(testcase.details)
			assert.Equal(t, testcase.expected, res)
		})
	}
}

func TestGroupByOwner(t *testing.T) {
	testcases := []struct {
		name     string
		details  []containerDetail
		expected map[config.Owner][]containerDetail
	}{
		{
			name:     "return empty map if no container detail",
			expected: make(map[config.Owner][]containerDetail),
		},
		{
			name: "returns map of grouped details by owner",
			details: []containerDetail{
				{
					sidecarConfig: config.SidecarConfig{
						Owner: config.Deployment,
					},
					containerStatus: corev1.ContainerStatus{
						State: corev1.ContainerState{
							Terminated: &corev1.ContainerStateTerminated{
								ExitCode: 137,
							},
						},
					},
				},
			},
			expected: map[config.Owner][]containerDetail{
				config.Deployment: []containerDetail{
					{
						sidecarConfig: config.SidecarConfig{
							Owner: config.Deployment,
						},
						containerStatus: corev1.ContainerStatus{
							State: corev1.ContainerState{
								Terminated: &corev1.ContainerStateTerminated{
									ExitCode: 137,
								},
							},
						},
					},
				},
			},
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			m := NewPodOwnerModifier(config.Config{})
			res := m.groupByOwner(testcase.details)
			assert.Equal(t, testcase.expected, res)
		})
	}
}
