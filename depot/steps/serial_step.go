package steps

import (
	"os"

	"code.cloudfoundry.org/lager"
	"github.com/tedsuo/ifrit"
)

type serialStep struct {
	steps  []ifrit.Runner
	logger lager.Logger
}

func NewSerial(logger lager.Logger, steps []ifrit.Runner) ifrit.Runner {
	return &serialStep{
		steps:  steps,
		logger: logger.Session("serial-step"),
	}
}

func (runner *serialStep) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	for i, action := range runner.steps {
		p := ifrit.Background(action)

		// wait for the last process to be ready
		if i == len(runner.steps)-1 {
			go func() {
				select {
				case <-p.Ready():
					close(ready)
				case <-p.Wait():
				}
			}()
		}

		select {
		case substepErr := <-p.Wait():
			if substepErr != nil {
				return substepErr
			}
		case signal := <-signals:
			runner.logger.Info("signaled")
			p.Signal(signal)
			return <-p.Wait()
		}
	}

	return nil
}
