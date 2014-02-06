package models

import (
	"encoding/json"
	"errors"
)

var InvalidActionConversion = errors.New("Invalid Action Conversion")

type CopyAction struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Extract  bool   `json:"extract"`
	Compress bool   `json:"compress"`
}

type RunAction struct {
	Script string `json:"script"`
}

type executorActionEnvelope struct {
	Name          string           `json:"action"`
	ActionPayload *json.RawMessage `json:"args"`
}

type ExecutorAction struct {
	Action interface{} `json:"-"`
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
