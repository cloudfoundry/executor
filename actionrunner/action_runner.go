package actionrunner

import (
	"fmt"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/vito/gordon"
)

type ActionRunnerInterface interface {
	Run(containerHandle string, actions []models.ExecutorAction) error
}

type ActionRunner struct {
	wardenClient gordon.Client
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
	runResponse, err := runner.wardenClient.Run(containerHandle, action.Script)

	if err != nil {
		return err
	}

	if runResponse.GetExitStatus() != 0 {
		return fmt.Errorf("Process returned with exit value: %d", runResponse.GetExitStatus())
	}

	return nil
}
