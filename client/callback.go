package client

import (
	"encoding/json"
	"github.com/cloudfoundry-incubator/executor/api"
)

type ContainerRunResult struct {
	Failed        bool
	FailureReason string
	Result        string
	Metadata      []byte
}

func NewContainerRunResultFromJSON(input []byte) (ContainerRunResult, error) {
	runResultJson := api.ContainerRunResult{}
	err := json.Unmarshal(input, &runResultJson)
	if err != nil {
		return ContainerRunResult{}, err
	}

	return ContainerRunResult{
		Failed:        runResultJson.Failed,
		FailureReason: runResultJson.FailureReason,
		Result:        runResultJson.Result,
		Metadata:      runResultJson.Metadata,
	}, nil
}
