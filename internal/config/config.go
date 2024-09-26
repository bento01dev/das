package config

import (
	"encoding/json"
	"fmt"
	"os"
)

type Owner string

func (o Owner) MarshalText() ([]byte, error) {
	switch o {
	case Deployment, ReplicaSet, DaemonSet:
		return []byte(o), nil
	default:
		return nil, fmt.Errorf("unknown type: %v", o)
	}
}

func (o *Owner) UnmarshalText(data []byte) error {
	s := string(data)
	switch s {
	case string(Deployment):
		*o = Deployment
		return nil
	case string(ReplicaSet):
		*o = ReplicaSet
		return nil
	case string(DaemonSet):
		*o = DaemonSet
		return nil
	default:
		return fmt.Errorf("unknown type: %s", s)
	}
}

const (
	Deployment Owner = "Deployment"
	ReplicaSet Owner = "ReplicaSet"
	DaemonSet  Owner = "DaemonSet"
)

type ResourceStep struct {
	RestartLimit int    `json:"restart_limit"`
	CPURequest   string `json:"cpu_request"`
	CPULimit     string `json:"cpu_limit"`
	MemRequest   string `json:"mem_request"`
	MemLimit     string `json:"mem_limit"`
}

type SidecarConfig struct {
	ErrCodes         []int          `json:"err_codes"`
	Owner            Owner          `json:"owner"`
	Steps            []ResourceStep `json:"steps"`
	CPUAnnotationKey string         `json:"cpu_annotation_key"`
	MemAnnotationKey string         `json:"mem_annotation_key"`
}

type Config struct {
	S3Bucket string                   `json:"s3_bucket"`
	Sidecars map[string]SidecarConfig `json:"sidecars"`
}

// TODO: add cue validation if needed
func Parse(configFilePath string) (Config, error) {
	var config Config
	f, err := os.Open(configFilePath)
	if err != nil {
		return config, fmt.Errorf("error opening config file in path %s: %w", configFilePath, err)
	}
	err = json.NewDecoder(f).Decode(&config)
	if err != nil {
		return config, fmt.Errorf("json parsing error for config in path %s: %w", configFilePath, err)
	}
	return config, nil
}

func (c Config) GetSidecarNames() []string {
	var names []string
	for name, _ := range c.Sidecars {
		names = append(names, name)
	}
	return names
}
