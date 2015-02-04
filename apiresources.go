package executor

import (
	"encoding/json"
	"errors"

	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

type State string

const (
	StateInvalid      State = ""
	StateReserved     State = "reserved"
	StateInitializing State = "initializing"
	StateCreated      State = "created"
	StateRunning      State = "running"
	StateCompleted    State = "completed"
)

type Container struct {
	Guid string `json:"guid"`

	State State `json:"state"`

	MemoryMB   int  `json:"memory_mb"`
	Privileged bool `json:"privileged"`
	DiskMB     int  `json:"disk_mb"`
	CPUWeight  uint `json:"cpu_weight"`

	Tags Tags `json:"tags,omitempty"`

	AllocatedAt int64 `json:"allocated_at"`

	RootFSPath string        `json:"root_fs"`
	ExternalIP string        `json:"external_ip"`
	Ports      []PortMapping `json:"ports"`
	Log        LogConfig     `json:"log"`

	StartTimeout uint          `json:"start_timeout"`
	Setup        models.Action `json:"setup"`
	Action       models.Action `json:"run"`
	Monitor      models.Action `json:"monitor"`

	Env []EnvironmentVariable `json:"env,omitempty"`

	RunResult ContainerRunResult `json:"run_result"`

	EgressRules []models.SecurityGroupRule `json:"egress_rules,omitempty"`
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

func (container *Container) UnmarshalJSON(payload []byte) error {
	mCon := &mContainer{InnerContainer: (*InnerContainer)(container)}
	err := json.Unmarshal(payload, mCon)
	if err != nil {
		return err
	}

	a, err := models.UnmarshalAction(mCon.ActionRaw)
	if err != nil {
		return err
	}
	container.Action = a

	if mCon.SetupRaw != nil {
		a, err = models.UnmarshalAction(*mCon.SetupRaw)
		if err != nil {
			return err
		}
		container.Setup = a
	}

	if mCon.MonitorRaw != nil {
		a, err = models.UnmarshalAction(*mCon.MonitorRaw)
		if err != nil {
			return err
		}
		container.Monitor = a
	}

	return nil
}

func (container Container) MarshalJSON() ([]byte, error) {
	actionRaw, err := models.MarshalAction(container.Action)
	if err != nil {
		return nil, err
	}

	var setupRaw, monitorRaw *json.RawMessage
	if container.Setup != nil {
		raw, err := models.MarshalAction(container.Setup)
		if err != nil {
			return nil, err
		}
		rm := json.RawMessage(raw)
		setupRaw = &rm
	}
	if container.Monitor != nil {
		raw, err := models.MarshalAction(container.Monitor)
		if err != nil {
			return nil, err
		}
		rm := json.RawMessage(raw)
		monitorRaw = &rm
	}

	innerContainer := InnerContainer(container)

	mCon := &mContainer{
		SetupRaw:       setupRaw,
		ActionRaw:      actionRaw,
		MonitorRaw:     monitorRaw,
		InnerContainer: &innerContainer,
	}

	return json.Marshal(mCon)
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
