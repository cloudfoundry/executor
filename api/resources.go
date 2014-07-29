package api

import (
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/tedsuo/ifrit"
)

const (
	StateReserved  = "reserved"
	StateCreated   = "created"
	StateCompleted = "completed"
)

type Container struct {
	Guid string `json:"guid"`

	// alloc
	MemoryMB int `json:"memory_mb"`
	DiskMB   int `json:"disk_mb"`

	AllocatedAt int64 `json:"allocated_at"`

	// init
	CpuPercent float64       `json:"cpu_percent"`
	Ports      []PortMapping `json:"ports"`
	Log        LogConfig     `json:"log"`

	// run
	Actions []models.ExecutorAction `json:"actions"`
	Env     []EnvironmentVariable   `json:"env,omitempty"`

	RunResult ContainerRunResult `json:"run_result"`

	// internally updated
	State           string        `json:"state"`
	ContainerHandle string        `json:"container_handle"`
	Process         ifrit.Process `json:"-"`
}

type EnvironmentVariable struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type LogConfig struct {
	Guid       string `json:"guid"`
	SourceName string `json:"source_name"`
	Index      *int   `json:"index"`
}

type PortMapping struct {
	ContainerPort uint32 `json:"container_port"`
	HostPort      uint32 `json:"host_port,omitempty"`
}

type ContainerAllocationRequest struct {
	MemoryMB int `json:"memory_mb"`
	DiskMB   int `json:"disk_mb"`
}

type ContainerInitializationRequest struct {
	CpuPercent float64       `json:"cpu_percent"`
	Ports      []PortMapping `json:"ports"`
	Log        LogConfig     `json:"log"`
}

type ContainerRunRequest struct {
	Actions     []models.ExecutorAction `json:"actions"`
	Env         []EnvironmentVariable   `json:"env,omitempty"`
	CompleteURL string                  `json:"complete_url"`
}

type ContainerRunResult struct {
	Guid string `json:"guid"`

	Failed        bool   `json:"failed"`
	FailureReason string `json:"failure_reason"`
	Result        string `json:"result"`
}

type ExecutorResources struct {
	MemoryMB   int `json:"memory_mb"`
	DiskMB     int `json:"disk_mb"`
	Containers int `json:"containers"`
}
