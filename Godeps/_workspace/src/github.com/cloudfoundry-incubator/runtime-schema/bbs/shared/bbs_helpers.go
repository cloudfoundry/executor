package shared

import (
	"time"
	"github.com/cloudfoundry/storeadapter"
)

func RetryIndefinitelyOnStoreTimeout(callback func() error) error {
	for {
		err := callback()

		if err == storeadapter.ErrorTimeout {
			time.Sleep(time.Second)
			continue
		}

		return err
	}
}
