package fake_bbs

import (
	"sync"
	"time"
)

type FakeConvergerBBS struct {
	callsToConverge int

	maintainConvergeInterval      time.Duration
	maintainConvergeExecutorID    string
	maintainConvergeStatusChannel <-chan bool
	maintainConvergeStopChannel   chan<- chan bool
	maintainConvergeLockError     error

	convergeTimeToClaimTasks time.Duration
	converganceInterval      time.Duration

	sync.RWMutex
}

func NewFakeConvergerBBS() *FakeConvergerBBS {
	fakeBBS := &FakeConvergerBBS{}
	return fakeBBS
}

func (fakeBBS *FakeConvergerBBS) ConvergeTask(timeToClaim time.Duration, converganceInterval time.Duration) {
	fakeBBS.Lock()
	defer fakeBBS.Unlock()

	fakeBBS.convergeTimeToClaimTasks = timeToClaim
	fakeBBS.converganceInterval = converganceInterval
	fakeBBS.callsToConverge++
}

func (fakeBBS *FakeConvergerBBS) MaintainConvergeLock(interval time.Duration, executorID string) (<-chan bool, chan<- chan bool, error) {
	status := make(chan bool)
	stop := make(chan chan bool)

	fakeBBS.RLock()
	err := fakeBBS.maintainConvergeLockError
	fakeBBS.RUnlock()

	if err != nil {
		return nil, nil, err
	}

	fakeBBS.Lock()
	fakeBBS.maintainConvergeInterval = interval
	fakeBBS.maintainConvergeExecutorID = executorID
	fakeBBS.maintainConvergeStatusChannel = status
	fakeBBS.maintainConvergeStopChannel = stop
	fakeBBS.Unlock()

	ticker := time.NewTicker(interval)
	go func() {
		status <- true
		for {
			select {
			case <-ticker.C:
				status <- true
			case release := <-stop:
				ticker.Stop()
				close(status)
				if release != nil {
					close(release)
				}

				return
			}
		}
	}()

	return status, stop, nil
}

func (fakeBBS *FakeConvergerBBS) SetMaintainConvergeLockError(err error) {
	fakeBBS.Lock()
	defer fakeBBS.Unlock()

	fakeBBS.maintainConvergeLockError = err
}

func (fakeBBS *FakeConvergerBBS) Stop() {
	fakeBBS.RLock()
	stopConverge := fakeBBS.maintainConvergeStopChannel
	fakeBBS.RUnlock()

	if stopConverge != nil {
		stopConverge <- nil
	}
}

func (fakeBBS *FakeConvergerBBS) ConvergeTimeToClaimTasks() time.Duration {
	fakeBBS.RLock()
	defer fakeBBS.RUnlock()

	return fakeBBS.convergeTimeToClaimTasks
}

func (fakeBBS *FakeConvergerBBS) CallsToConverge() int {
	fakeBBS.RLock()
	defer fakeBBS.RUnlock()

	return fakeBBS.callsToConverge
}
