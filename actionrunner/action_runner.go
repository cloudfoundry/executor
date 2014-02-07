package actionrunner

import (
	"fmt"
	"time"

	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/vito/gordon"
	"github.com/vito/gordon/warden"
)

type ActionRunnerInterface interface {
	Run(containerHandle string, actions []models.ExecutorAction) error
}

type ActionRunner struct {
	wardenClient gordon.Client
}

type RunActionTimeoutError struct {
	Action models.RunAction
}

func (e RunActionTimeoutError) Error() string {
	return fmt.Sprintf("action timed out after %s", e.Action.Timeout)
}

func New(wardenClient gordon.Client) *ActionRunner {
	return &ActionRunner{
		wardenClient: wardenClient,
	}
}

func (runner *ActionRunner) Run(containerHandle string, actions []models.ExecutorAction) error {
	for _, action := range actions {
		var err error
		switch a := action.Action.(type) {
		case models.RunAction:
			err = runner.performRunAction(containerHandle, a)
		case models.CopyAction:
			// Copy
		}
		if err != nil {
			return err
		}
	}

	return nil
}

func (runner *ActionRunner) performRunAction(containerHandle string, action models.RunAction) error {
	response := make(chan *warden.RunResponse, 1)
	errChan := make(chan error, 1)

	var timeoutChan <-chan time.Time

	if action.Timeout != 0 {
		timeoutChan = time.After(action.Timeout)
	}

	go func() {
		runResponse, err := runner.wardenClient.Run(containerHandle, action.Script)

		if err != nil {
			errChan <- err
		} else {
			response <- runResponse
		}
	}()

	select {
	case runResponse := <-response:
		if runResponse.GetExitStatus() != 0 {
			return fmt.Errorf("Process returned with exit value: %d", runResponse.GetExitStatus())
		}

		return nil

	case err := <-errChan:
		return err

	case <-timeoutChan:
		return RunActionTimeoutError{Action: action}
	}

	panic("unreachable")
}
