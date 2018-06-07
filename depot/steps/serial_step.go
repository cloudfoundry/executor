package steps

import (
	"os"

	"github.com/tedsuo/ifrit"
)

type serialStep struct {
	steps []ifrit.Runner
}

func NewSerial(steps []ifrit.Runner) ifrit.Runner {
	return &serialStep{
		steps: steps,
	}
}

func (runner *serialStep) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	close(ready)
	for _, action := range runner.steps {
		p := ifrit.Background(action)
		select {
		case substepErr := <-p.Wait():
			if substepErr != nil {
				return substepErr
			}
		case signal := <-signals:
			p.Signal(signal)
			return ErrCancelled
		}
	}

	return nil
}
