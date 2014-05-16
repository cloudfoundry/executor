package checks

import (
	"encoding/json"
	"fmt"
)

type Check interface {
	Check() bool
}

type ErrUnknownCheck struct {
	Name string
}

func (err ErrUnknownCheck) Error() string {
	return fmt.Sprintf("unknown check type: %s", err.Name)
}

type checkEnvelope struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

func Parse(payload json.RawMessage) (Check, error) {
	var envelope checkEnvelope

	err := json.Unmarshal(payload, &envelope)
	if err != nil {
		return nil, err
	}

	switch envelope.Name {
	case "dial":
		var check dial

		err := json.Unmarshal(envelope.Args, &check)
		if err != nil {
			return nil, err
		}

		return &check, nil
	}

	return nil, ErrUnknownCheck{envelope.Name}
}
