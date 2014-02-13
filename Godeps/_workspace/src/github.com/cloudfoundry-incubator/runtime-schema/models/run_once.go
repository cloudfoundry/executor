package models

import (
	"encoding/json"
)

type RunOnce struct {
	Guid     string           `json:"guid"`
	Actions  []ExecutorAction `json:"actions"`
	Stack    string           `json:"stack"`
	MemoryMB int              `json:"memory_mb"`
	DiskMB   int              `json:"disk_mb"`
	Log      LogConfig        `json:"log"`

	// this is so that any stager can process a complete event,
	// because the CC <-> Stager interaction is a one-to-one request-response
	//
	// ideally staging completion is a "broadcast" event instead and this goes away
	ReplyTo string `json:"reply_to"`

	ExecutorID string `json:"executor_id"`

	ContainerHandle string `json:"container_handle"`

	Failed        bool   `json:"failed"`
	FailureReason string `json:"failure_reason"`
}

type LogConfig struct {
	Guid  string `json:"guid"`
	Type  string `json:"type"`
	Index int    `json:"index"`
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
