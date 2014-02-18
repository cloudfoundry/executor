package actionrunner

import (
	"fmt"
	"time"

	"github.com/vito/gordon"
	"github.com/vito/gordon/warden"

	"github.com/cloudfoundry-incubator/executor/actionrunner/logstreamer"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

type RunRunner struct {
	wardenClient  gordon.Client
	backendPlugin BackendPlugin
}

func NewRunRunner(wardenClient gordon.Client, backendPlugin BackendPlugin) *RunRunner {
	return &RunRunner{
		wardenClient:  wardenClient,
		backendPlugin: backendPlugin,
	}
}

func (runner *RunRunner) perform(containerHandle string, streamer logstreamer.LogStreamer, action models.RunAction) error {
	exitStatusChan := make(chan uint32, 1)
	errChan := make(chan error, 1)

	var timeoutChan <-chan time.Time

	if action.Timeout != 0 {
		timeoutChan = time.After(action.Timeout)
	}

	go func() {
		_, stream, err := runner.wardenClient.Run(
			containerHandle,
			runner.backendPlugin.BuildRunScript(action),
		)

		if err != nil {
			errChan <- err
			return
		}

		for payload := range stream {
			if payload.ExitStatus != nil {
				if streamer != nil {
					streamer.Flush()
				}
				exitStatusChan <- payload.GetExitStatus()
				break
			}

			if streamer != nil {
				switch *payload.Source {
				case warden.ProcessPayload_stdout:
					streamer.StreamStdout(payload.GetData())
				case warden.ProcessPayload_stderr:
					streamer.StreamStderr(payload.GetData())
				}
			}
		}
	}()

	select {
	case exitStatus := <-exitStatusChan:
		if exitStatus != 0 {
			return fmt.Errorf("Process returned with exit value: %d", exitStatus)
		}

		return nil

	case err := <-errChan:
		return err

	case <-timeoutChan:
		return RunActionTimeoutError{Action: action}
	}

	panic("unreachable")
}
