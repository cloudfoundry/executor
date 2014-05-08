package fake_client

import (
	"encoding/json"
	"github.com/cloudfoundry-incubator/executor/api"
	"github.com/cloudfoundry-incubator/executor/client"
)

func MarshalContainerRunResult(result client.ContainerRunResult) ([]byte, error) {
	runResultJson := api.ContainerRunResult{
		Failed:        result.Failed,
		FailureReason: result.FailureReason,
		Metadata:      result.Metadata,
		Result:        result.Result,
	}
	return json.Marshal(&runResultJson)
}
