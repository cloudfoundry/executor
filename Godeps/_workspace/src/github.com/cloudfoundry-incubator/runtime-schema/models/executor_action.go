package models

import (
	"encoding/json"
	"errors"
	"time"
)

var InvalidActionConversion = errors.New("Invalid Action Conversion")

type CopyAction struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Extract  bool   `json:"extract"`
	Compress bool   `json:"compress"`
}

type RunAction struct {
	Script  string
	Env     map[string]string
	Timeout time.Duration
}

type executorActionEnvelope struct {
	Name          string           `json:"action"`
	ActionPayload *json.RawMessage `json:"args"`
}

type ExecutorAction struct {
	Action interface{} `json:"-"`
}

type runActionSerialized struct {
	Script           string            `json:"script"`
	TimeoutInSeconds uint64            `json:"timeout_in_seconds"`
	Env              map[string]string `json:"env"`
}

func (a RunAction) MarshalJSON() ([]byte, error) {
	return json.Marshal(runActionSerialized{
		Script:           a.Script,
		TimeoutInSeconds: uint64(a.Timeout / time.Second),
		Env:              a.Env,
	})
}

func (a *RunAction) UnmarshalJSON(payload []byte) error {
	var intermediate runActionSerialized

	err := json.Unmarshal(payload, &intermediate)
	if err != nil {
		return err
	}

	a.Script = intermediate.Script
	a.Timeout = time.Duration(intermediate.TimeoutInSeconds) * time.Second
	a.Env = intermediate.Env

	return nil
}

func (a ExecutorAction) MarshalJSON() ([]byte, error) {
	var envelope executorActionEnvelope

	payload, err := json.Marshal(a.Action)

	if err != nil {
		return nil, err
	}

	switch a.Action.(type) {
	case CopyAction:
		envelope.Name = "copy"
	case RunAction:
		envelope.Name = "run"
	default:
		return nil, InvalidActionConversion
	}

	envelope.ActionPayload = (*json.RawMessage)(&payload)

	return json.Marshal(envelope)
}

func (a *ExecutorAction) UnmarshalJSON(bytes []byte) error {
	var envelope executorActionEnvelope

	err := json.Unmarshal(bytes, &envelope)
	if err != nil {
		return err
	}

	switch envelope.Name {
	case "copy":
		copyAction := CopyAction{}
		err = json.Unmarshal(*envelope.ActionPayload, &copyAction)
		a.Action = copyAction
	case "run":
		runAction := RunAction{}
		err = json.Unmarshal(*envelope.ActionPayload, &runAction)
		a.Action = runAction
	default:
		err = InvalidActionConversion
	}

	return err
}
