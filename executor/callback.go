package executor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/cloudfoundry-incubator/executor/api"
	"github.com/cloudfoundry/gosteno"
)

const MAX_CALLBACK_ATTEMPTS = 42

type Callback struct {
	URL     string
	Payload api.ContainerRunResult
	Logger  *gosteno.Logger
}

func (c *Callback) Run(sigChan <-chan os.Signal, readyChan chan<- struct{}) error {
	resultPayload, err := json.Marshal(c.Payload)
	if err != nil {
		c.Logger.Errord(map[string]interface{}{
			"error": err.Error(),
		}, "executor.run-action-callback.json-marshal-failed")
		return err
	}

	close(readyChan)

	for i := 1; i <= MAX_CALLBACK_ATTEMPTS; i++ {
		errChan := make(chan error, 1)
		go func() {
			errChan <- performCompleteCallback(c.URL, resultPayload)
		}()

		var err error

		select {
		case <-sigChan:
			return nil
		case err = <-errChan:
			// break if we succeed
			if err == nil {
				return nil
			}
		}

		c.Logger.Warnd(map[string]interface{}{
			"error": err.Error(),
		}, "executor.run-action-callback.failed")

		time.Sleep(time.Duration(i) * 500 * time.Millisecond)
	}

	c.Logger.Errord(map[string]interface{}{
		"error": err.Error(),
	}, "executor.run-action-callback.callback-failed")

	return err
}

func performCompleteCallback(completeURL string, payload []byte) error {
	resultRequest, err := http.NewRequest("PUT", completeURL, bytes.NewBuffer(payload))
	if err != nil {
		return err
	}

	resultRequest.Header.Set("Content-Type", "application/json")

	res, err := http.DefaultClient.Do(resultRequest)
	if err != nil {
		return err
	}

	res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("Callback failed with status code %d", res.StatusCode)
	}

	return nil
}
