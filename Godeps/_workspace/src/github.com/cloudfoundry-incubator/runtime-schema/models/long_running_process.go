package models

import "encoding/json"

type TransitionalLRPState int

const (
	TransitionalLRPStateInvalid TransitionalLRPState = iota
	TransitionalLRPStateDesired
	TransitionalLRPStateRunning
)

type TransitionalLongRunningProcess struct {
	Guid     string               `json:"guid"`
	Stack    string               `json:"stack"`
	Actions  []ExecutorAction     `json:"actions"`
	Log      LogConfig            `json:"log"`
	State    TransitionalLRPState `json:"state"`
	MemoryMB int                  `json:"memory_mb"`
	DiskMB   int                  `json:"disk_mb"`
	Ports    []PortMapping        `json:"ports"`
}

type PortMapping struct {
	ContainerPort int `json:"container_port"`
	HostPort      int `json:"host_port,omitempty"`
}

func NewTransitionalLongRunningProcessFromJSON(payload []byte) (TransitionalLongRunningProcess, error) {
	var task TransitionalLongRunningProcess

	err := json.Unmarshal(payload, &task)
	if err != nil {
		return TransitionalLongRunningProcess{}, err
	}

	return task, nil
}

func (self TransitionalLongRunningProcess) ToJSON() []byte {
	bytes, err := json.Marshal(self)
	if err != nil {
		panic(err)
	}

	return bytes
}

///

type LRPStartAuctionState int

const (
	LRPStartAuctionStateInvalid LRPStartAuctionState = iota
	LRPStartAuctionStatePending
	LRPStartAuctionStateClaimed
)

type LRPStartAuction struct {
	Guid         string `json:"guid"`
	InstanceGuid string `json:"instance_guid"`

	DiskMB   int `json:"disk_mb"`
	MemoryMB int `json:"memory_mb"`

	Stack   string           `json:"stack"`
	Actions []ExecutorAction `json:"actions"`
	Log     LogConfig        `json:"log"`
	Ports   []PortMapping    `json:"ports"`

	Index int `json:"index"`

	State LRPStartAuctionState `json:"state"`
}

func NewLRPStartAuctionFromJSON(payload []byte) (LRPStartAuction, error) {
	var task LRPStartAuction

	err := json.Unmarshal(payload, &task)
	if err != nil {
		return LRPStartAuction{}, err
	}

	return task, nil
}

func (self LRPStartAuction) ToJSON() []byte {
	bytes, err := json.Marshal(self)
	if err != nil {
		panic(err)
	}

	return bytes
}

///

type LRP struct {
	ProcessGuid  string `json:"process_guid"`
	InstanceGuid string `json:"instance_guid"`

	Index int `json:"index"`

	Host  string        `json:"host"`
	Ports []PortMapping `json:"ports"`
}

func NewLRPFromJSON(payload []byte) (LRP, error) {
	var task LRP

	err := json.Unmarshal(payload, &task)
	if err != nil {
		return LRP{}, err
	}

	return task, nil
}

func (self LRP) ToJSON() []byte {
	bytes, err := json.Marshal(self)
	if err != nil {
		panic(err)
	}

	return bytes
}
