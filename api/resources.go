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
	Guid     string                  `json:"guid"`
	MemoryMB int                     `json:"memory_mb"`
	DiskMB   int                     `json:"disk_mb"`
	Actions  []models.ExecutorAction `json:"actions"`

	CpuPercent      float64            `json:"cpu_percent"`
	Ports           []PortMapping      `json:"ports"`
	State           string             `json:"state"`
	ContainerHandle string             `json:"container_handle"`
	Log             models.LogConfig   `json:"log"`
	AllocatedAt     int64              `json:"allocated_at"`
	RunResult       ContainerRunResult `json:"run_result"`

	Process ifrit.Process `json:"-"`
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
	CpuPercent float64          `json:"cpu_percent"`
	Ports      []PortMapping    `json:"ports"`
	Log        models.LogConfig `json:"log"`
}

type ContainerRunRequest struct {
	Actions     []models.ExecutorAction `json:"actions"`
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
