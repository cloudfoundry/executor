package executor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/cloudfoundry-incubator/executor/api"
	"github.com/cloudfoundry-incubator/executor/registry"
	"github.com/cloudfoundry-incubator/executor/sequence"
	"github.com/cloudfoundry-incubator/garden/warden"
	"github.com/cloudfoundry/gosteno"
	"github.com/tedsuo/ifrit"
)

func RunSequence(
	completeURL string,
	runWaitGroup *sync.WaitGroup,
	runCancelChan chan struct{},
	wardenClient warden.Client,
	Registry registry.Registry,
	registration api.Container,
	logger *gosteno.Logger,
	seq sequence.Step,
	result *string,
) {
	defer runWaitGroup.Done()

	run := ifrit.Envoke(&Run{
		WardenClient: wardenClient,
		Registry:     Registry,
		Registration: registration,
		Sequence:     seq,
		Result:       result,
		Logger:       logger,
	})

	var err error
	select {
	case <-runCancelChan:
		run.Signal(os.Interrupt)
		err = <-run.Wait()
	case err = <-run.Wait():
	}

	if completeURL == "" {
		return
	}

	payload := api.ContainerRunResult{
		Guid:   registration.Guid,
		Result: *result,
	}
	if err != nil {
		payload.Failed = true
		payload.FailureReason = err.Error()
	}

	callback := ifrit.Envoke(&Callback{
		URL:     completeURL,
		Payload: payload,
		Logger:  logger,
	})

	<-callback.Wait()

}

type Run struct {
	WardenClient warden.Client
	Registry     registry.Registry
	Registration api.Container
	Logger       *gosteno.Logger
	Sequence     sequence.Step
	Result       *string
}

func (r *Run) Run(sigChan <-chan os.Signal, readyChan chan<- struct{}) error {
	seqComplete := make(chan error)

	go func() {
		seqComplete <- r.Sequence.Perform()
	}()

	close(readyChan)

	for {
		select {
		case <-sigChan:
			r.Logger.Info("executor.perform-action.cancelled")
			sigChan = nil
			r.Sequence.Cancel()

		case seqErr := <-seqComplete:
			if seqErr != nil {
				r.Logger.Errord(map[string]interface{}{
					"error": seqErr.Error(),
				}, "executor.perform-sequence.failed")
			}

			err := r.WardenClient.Destroy(r.Registration.ContainerHandle)
			if err != nil {
				r.Logger.Warnd(map[string]interface{}{
					"error":  err.Error(),
					"handle": r.Registration.ContainerHandle,
				}, "executor.run-action.destroy-failed")
			}

			r.Registry.Delete(r.Registration.Guid)

			return seqErr
		}
	}
}

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
