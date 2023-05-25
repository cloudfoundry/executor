package steps

import (
	"fmt"
	"os"

	"code.cloudfoundry.org/executor/depot/log_streamer"
	"github.com/tedsuo/ifrit"
)

type readinessHealthCheckStep struct {
	untilReadyCheck   ifrit.Runner
	untilFailureCheck ifrit.Runner
	logStreamer       log_streamer.LogStreamer
}

func NewReadinessHealthCheckStep(
	untilReadyCheck ifrit.Runner,
	untilFailureCheck ifrit.Runner,
	logStreamer log_streamer.LogStreamer,
) ifrit.Runner {
	return &readinessHealthCheckStep{
		untilReadyCheck:   untilReadyCheck,
		untilFailureCheck: untilFailureCheck,
		logStreamer:       logStreamer,
	}
}

func (step *readinessHealthCheckStep) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	fmt.Fprint(step.logStreamer.Stdout(), "Starting readiness health monitoring of container\n")

	err := step.runUntilReadyProcess(signals)
	if err != nil {
		return err
	}

	close(ready)

	err = step.runUntilFailureProcess(signals)
	if err != nil {
		return err
	}

	return nil
}

func (step *readinessHealthCheckStep) runUntilReadyProcess(signals <-chan os.Signal) error {
	untilReadyProcess := ifrit.Background(step.untilReadyCheck)
	select {
	case err := <-untilReadyProcess.Wait():
		if err != nil {
			fmt.Fprint(step.logStreamer.Stdout(), "Failed to run the untilReady check\n")
			return err
		}
		fmt.Fprint(step.logStreamer.Stdout(), "App is ready!\n")
		return nil
	case s := <-signals:
		untilReadyProcess.Signal(s)
		<-untilReadyProcess.Wait()
		return new(CancelledError)
	}
}

func (step *readinessHealthCheckStep) runUntilFailureProcess(signals <-chan os.Signal) error {
	untilFailureProcess := ifrit.Background(step.untilFailureCheck)
	select {
	case err := <-untilFailureProcess.Wait():
		if err != nil {
			fmt.Fprint(step.logStreamer.Stdout(), "Oh no! The app is not ready anymore\n")
			return nil
		}
		return nil // TODO: would this case ever happen? how should we handle this?
	case s := <-signals:
		untilFailureProcess.Signal(s)
		<-untilFailureProcess.Wait()
		return new(CancelledError)
	}
}

// if err := dec.Decode(&val); err != nil {
//     if serr, ok := err.(*json.SyntaxError); ok {
//         line, col := findLine(f, serr.Offset)
//         return fmt.Errorf("%s:%d:%d: %v", f.Name(), line, col, err)
//     }
//     return err
// }V
