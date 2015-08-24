package executor

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/cloudfoundry-incubator/bbs/models"
)

type State string
type DiskLimitScope uint8

const (
	StateInvalid      State = ""
	StateReserved     State = "reserved"
	StateInitializing State = "initializing"
	StateCreated      State = "created"
	StateRunning      State = "running"
	StateCompleted    State = "completed"
)
const (
	ExclusiveDiskLimit DiskLimitScope = iota
	TotalDiskLimit     DiskLimitScope = iota
)

type Container struct {
	Guid string `json:"guid"`

	State State `json:"state"`

	Privileged bool `json:"privileged"`

	MemoryMB  int            `json:"memory_mb"`
	DiskMB    int            `json:"disk_mb"`
	DiskScope DiskLimitScope `json:"disk_scope,omitempty"`

	CPUWeight uint `json:"cpu_weight"`

	Tags Tags `json:"tags,omitempty"`

	AllocatedAt int64 `json:"allocated_at"`

	RootFSPath string        `json:"rootfs"`
	ExternalIP string        `json:"external_ip"`
	Ports      []PortMapping `json:"ports"`

	LogConfig     LogConfig     `json:"log_config"`
	MetricsConfig MetricsConfig `json:"metrics_config"`

	StartTimeout uint           `json:"start_timeout"`
	Setup        *models.Action `json:"setup"`
	Action       *models.Action `json:"run"`
	Monitor      *models.Action `json:"monitor"`

	Env []EnvironmentVariable `json:"env,omitempty"`

	RunResult ContainerRunResult `json:"run_result"`

	EgressRules []*models.SecurityGroupRule `json:"egress_rules,omitempty"`
}

func (c *Container) HasTags(tags Tags) bool {
	if c.Tags == nil {
		return tags == nil
	}

	if tags == nil {
		return false
	}

	for key, val := range tags {
		v, ok := c.Tags[key]
		if !ok || val != v {
			return false
		}
	}

	return true
}

type InnerContainer Container

type mContainer struct {
	SetupRaw   *json.RawMessage `json:"setup"`
	ActionRaw  json.RawMessage  `json:"run"`
	MonitorRaw *json.RawMessage `json:"monitor"`

	*InnerContainer
}

type EnvironmentVariable struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type ContainerMetrics struct {
	MemoryUsageInBytes uint64        `json:"memory_usage_in_bytes"`
	DiskUsageInBytes   uint64        `json:"disk_usage_in_bytes"`
	TimeSpentInCPU     time.Duration `json:"time_spent_in_cpu"`
}

type MetricsConfig struct {
	Guid  string `json:"guid"`
	Index int    `json:"index"`
}

type Metrics struct {
	MetricsConfig
	ContainerMetrics
}

type LogConfig struct {
	Guid       string `json:"guid"`
	Index      int    `json:"index"`
	SourceName string `json:"source_name"`
}

type PortMapping struct {
	ContainerPort uint16 `json:"container_port"`
	HostPort      uint16 `json:"host_port,omitempty"`
}

type ContainerRunResult struct {
	Failed        bool   `json:"failed"`
	FailureReason string `json:"failure_reason"`

	Stopped bool `json:"stopped"`
}

type ExecutorResources struct {
	MemoryMB   int `json:"memory_mb"`
	DiskMB     int `json:"disk_mb"`
	Containers int `json:"containers"`
}

type Tags map[string]string

type Event interface {
	EventType() EventType
}

type EventType string

var ErrUnknownEventType = errors.New("unknown event type")

const (
	EventTypeInvalid EventType = ""

	EventTypeContainerComplete EventType = "container_complete"
	EventTypeContainerRunning  EventType = "container_running"
	EventTypeContainerReserved EventType = "container_reserved"
)

type LifecycleEvent interface {
	Container() Container
	lifecycleEvent()
}

type ContainerCompleteEvent struct {
	RawContainer Container `json:"container"`
}

func NewContainerCompleteEvent(container Container) ContainerCompleteEvent {
	return ContainerCompleteEvent{
		RawContainer: container,
	}
}

func (ContainerCompleteEvent) EventType() EventType   { return EventTypeContainerComplete }
func (e ContainerCompleteEvent) Container() Container { return e.RawContainer }
func (ContainerCompleteEvent) lifecycleEvent()        {}

type ContainerRunningEvent struct {
	RawContainer Container `json:"container"`
}

func NewContainerRunningEvent(container Container) ContainerRunningEvent {
	return ContainerRunningEvent{
		RawContainer: container,
	}
}

func (ContainerRunningEvent) EventType() EventType   { return EventTypeContainerRunning }
func (e ContainerRunningEvent) Container() Container { return e.RawContainer }
func (ContainerRunningEvent) lifecycleEvent()        {}

type ContainerReservedEvent struct {
	RawContainer Container `json:"container"`
}

func NewContainerReservedEvent(container Container) ContainerReservedEvent {
	return ContainerReservedEvent{
		RawContainer: container,
	}
}

func (ContainerReservedEvent) EventType() EventType   { return EventTypeContainerReserved }
func (e ContainerReservedEvent) Container() Container { return e.RawContainer }
func (ContainerReservedEvent) lifecycleEvent()        {}
