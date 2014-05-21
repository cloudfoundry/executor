package api

import "github.com/cloudfoundry-incubator/runtime-schema/models"

const (
	StateReserved = "reserved"
	StateCreated  = "created"
)

type Container struct {
	Guid string `json:"guid"`

	ExecutorGuid    string            `json:"executor_guid"`
	MemoryMB        int               `json:"memory_mb"`
	DiskMB          int               `json:"disk_mb"`
	CpuPercent      float64           `json:"cpu_percent"`
	Ports           []PortMapping     `json:"ports"`
	State           string            `json:"state"`
	ContainerHandle string            `json:"container_handle"`
	Log             models.LogConfig  `json:"log"`
	Metadata        map[string]string `json:"metadata"`
}

type PortMapping struct {
	ContainerPort uint32 `json:"container_port"`
	HostPort      uint32 `json:"host_port,omitempty"`
}

type ContainerAllocationRequest struct {
	MemoryMB int               `json:"memory_mb"`
	DiskMB   int               `json:"disk_mb"`
	Metadata map[string]string `json:"metadata"`
}

type ContainerInitializationRequest struct {
	CpuPercent float64          `json:"cpu_percent"`
	Ports      []PortMapping    `json:"ports"`
	Log        models.LogConfig `json:"log"`
}

type ContainerInitializeResult struct {
	Ports []PortMapping `json:"ports"`
}

type ContainerRunRequest struct {
	Actions     []models.ExecutorAction `json:"actions"`
	Metadata    []byte                  `json:"metadata"`
	CompleteURL string                  `json:"complete_url"`
}

type ContainerRunResult struct {
	Failed        bool   `json:"failed"`
	FailureReason string `json:"failure_reason"`
	Result        string `json:"result"`
	Metadata      []byte `json:"metadata"`
}

type ExecutorResources struct {
	MemoryMB   int `json:"memory_mb"`
	DiskMB     int `json:"disk_mb"`
	Containers int `json:"containers"`
}
