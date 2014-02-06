package models

import (
	"encoding/json"
)

type RunOnce struct {
	Guid string `json:"guid"`

	// this is so that any stager can process a complete event,
	// because the CC <-> Stager interaction is a one-to-one request-response
	//
	// ideally staging completion is a "broadcast" event instead and this goes away
	ReplyTo string `json:"reply_to"`

	Stack string `json:"stack"`

	ExecutorID string `json:"executor_id"`

	Actions []ExecutorAction `json:"actions"`

	ContainerHandle string `json:"container_handle"`

	Failed        bool   `json:"failed"`
	FailureReason string `json:"failure_reason"`

	MemoryMB int `json:"memory_mb"`
	DiskMB   int `json:"disk_mb"`
}

func NewRunOnceFromJSON(payload []byte) (RunOnce, error) {
	var runOnce RunOnce

	err := json.Unmarshal(payload, &runOnce)
	if err != nil {
		return RunOnce{}, err
	}

	return runOnce, nil
}

func (self RunOnce) ToJSON() []byte {
	bytes, err := json.Marshal(self)
	if err != nil {
		panic(err)
	}

	return bytes
}
