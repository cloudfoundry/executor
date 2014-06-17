package fake_bbs

import (
	"sync"
	"time"
)

type FakeConvergerBBS struct {
	callsToConvergeTasks            int
	callsToConvergeLRPs             int
	callsToConvergeLRPStartAuctions int
	callsToConvergeLRPStopAuctions  int

	ConvergeLockStatusChan chan bool
	ConvergeLockStopChan   chan chan bool

	maintainConvergeInterval      time.Duration
	maintainConvergeExecutorID    string
	maintainConvergeStatusChannel <-chan bool
	maintainConvergeStopChannel   chan<- chan bool
	maintainConvergeLockError     error

	convergeTimeToClaimTasks time.Duration
	taskConvergenceInterval  time.Duration

	sync.RWMutex
}

func NewFakeConvergerBBS() *FakeConvergerBBS {
	fakeBBS := &FakeConvergerBBS{
		ConvergeLockStatusChan: make(chan bool),
		ConvergeLockStopChan:   make(chan chan bool),
	}
	return fakeBBS
}

func (fakeBBS *FakeConvergerBBS) ConvergeLRPs() {
	fakeBBS.Lock()
	defer fakeBBS.Unlock()

	fakeBBS.callsToConvergeLRPs++
}

func (fakeBBS *FakeConvergerBBS) CallsToConvergeLRPs() int {
	fakeBBS.RLock()
	defer fakeBBS.RUnlock()

	return fakeBBS.callsToConvergeLRPs
}

func (fakeBBS *FakeConvergerBBS) ConvergeLRPStartAuctions(kickPendingDuration time.Duration, expireClaimedDuration time.Duration) {
	fakeBBS.Lock()
	defer fakeBBS.Unlock()

	fakeBBS.callsToConvergeLRPStartAuctions++
}

func (fakeBBS *FakeConvergerBBS) CallsToConvergeLRPStartAuctions() int {
	fakeBBS.RLock()
	defer fakeBBS.RUnlock()

	return fakeBBS.callsToConvergeLRPStartAuctions
}

func (fakeBBS *FakeConvergerBBS) ConvergeLRPStopAuctions(kickPendingDuration time.Duration, expireClaimedDuration time.Duration) {
	fakeBBS.Lock()
	defer fakeBBS.Unlock()

	fakeBBS.callsToConvergeLRPStopAuctions++
}

func (fakeBBS *FakeConvergerBBS) CallsToConvergeLRPStopAuctions() int {
	fakeBBS.RLock()
	defer fakeBBS.RUnlock()

	return fakeBBS.callsToConvergeLRPStopAuctions
}

func (fakeBBS *FakeConvergerBBS) ConvergeTask(timeToClaim time.Duration, taskConvergenceInterval time.Duration) {
	fakeBBS.Lock()
	defer fakeBBS.Unlock()

	fakeBBS.convergeTimeToClaimTasks = timeToClaim
	fakeBBS.taskConvergenceInterval = taskConvergenceInterval
	fakeBBS.callsToConvergeTasks++
}

func (fakeBBS *FakeConvergerBBS) ConvergeTimeToClaimTasks() time.Duration {
	fakeBBS.RLock()
	defer fakeBBS.RUnlock()

	return fakeBBS.convergeTimeToClaimTasks
}

func (fakeBBS *FakeConvergerBBS) CallsToConvergeTasks() int {
	fakeBBS.RLock()
	defer fakeBBS.RUnlock()

	return fakeBBS.callsToConvergeTasks
}

func (fakeBBS *FakeConvergerBBS) MaintainConvergeLock(interval time.Duration, executorID string) (<-chan bool, chan<- chan bool, error) {
	fakeBBS.Lock()
	defer fakeBBS.Unlock()

	return fakeBBS.ConvergeLockStatusChan, fakeBBS.ConvergeLockStopChan, fakeBBS.maintainConvergeLockError
}

func (fakeBBS *FakeConvergerBBS) SetMaintainConvergeLockError(err error) {
	fakeBBS.Lock()
	defer fakeBBS.Unlock()

	fakeBBS.maintainConvergeLockError = err
}
