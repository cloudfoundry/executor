package fake_bbs

import (
	"sync"
	"time"

	"github.com/cloudfoundry-incubator/runtime-schema/bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

type FakeExecutorBBS struct {
	callsToConverge int

	maintainConvergeInterval      time.Duration
	maintainConvergeExecutorID    string
	maintainConvergeStatusChannel <-chan bool
	maintainConvergeStopChannel   chan<- chan bool
	maintainConvergeLockError     error

	maintainingPresenceHeartbeatInterval time.Duration
	maintainingPresenceExecutorID        string
	maintainingPresencePresence          *FakePresence
	maintainingPresenceError             error

	claimedRunOnces []*models.RunOnce
	claimRunOnceErr error

	startedRunOnces []*models.RunOnce
	startRunOnceErr error

	completedRunOnces           []*models.RunOnce
	completeRunOnceErr          error
	convergeTimeToClaimRunOnces time.Duration

	sync.RWMutex
}

func NewFakeExecutorBBS() *FakeExecutorBBS {
	return &FakeExecutorBBS{}
}

func (fakeBBS *FakeExecutorBBS) MaintainExecutorPresence(heartbeatInterval time.Duration, executorID string) (bbs.Presence, <-chan bool, error) {
	fakeBBS.maintainingPresenceHeartbeatInterval = heartbeatInterval
	fakeBBS.maintainingPresenceExecutorID = executorID
	fakeBBS.maintainingPresencePresence = &FakePresence{}
	status, _ := fakeBBS.maintainingPresencePresence.Maintain(heartbeatInterval)

	return fakeBBS.maintainingPresencePresence, status, fakeBBS.maintainingPresenceError
}

func (fakeBBS *FakeExecutorBBS) WatchForDesiredRunOnce() (<-chan *models.RunOnce, chan<- bool, <-chan error) {
	return nil, nil, nil
}

func (fakeBBS *FakeExecutorBBS) ClaimRunOnce(runOnce *models.RunOnce, executorID string) error {
	runOnce.ExecutorID = executorID

	fakeBBS.RLock()
	err := fakeBBS.claimRunOnceErr
	fakeBBS.RUnlock()

	if err != nil {
		return err
	}

	fakeBBS.Lock()
	fakeBBS.claimedRunOnces = append(fakeBBS.claimedRunOnces, runOnce)
	fakeBBS.Unlock()

	return nil
}

func (fakeBBS *FakeExecutorBBS) ClaimedRunOnces() []*models.RunOnce {
	fakeBBS.RLock()
	defer fakeBBS.RUnlock()

	claimed := make([]*models.RunOnce, len(fakeBBS.claimedRunOnces))
	copy(claimed, fakeBBS.claimedRunOnces)

	return claimed
}

func (fakeBBS *FakeExecutorBBS) SetClaimRunOnceErr(err error) {
	fakeBBS.Lock()
	defer fakeBBS.Unlock()

	fakeBBS.claimRunOnceErr = err
}

func (fakeBBS *FakeExecutorBBS) StartRunOnce(runOnce *models.RunOnce, containerHandle string) error {
	fakeBBS.RLock()
	err := fakeBBS.startRunOnceErr
	fakeBBS.RUnlock()

	if err != nil {
		return err
	}

	runOnce.ContainerHandle = containerHandle

	fakeBBS.Lock()
	fakeBBS.startedRunOnces = append(fakeBBS.startedRunOnces, runOnce)
	fakeBBS.Unlock()

	return nil
}

func (fakeBBS *FakeExecutorBBS) StartedRunOnces() []*models.RunOnce {
	fakeBBS.RLock()
	defer fakeBBS.RUnlock()

	started := make([]*models.RunOnce, len(fakeBBS.startedRunOnces))
	copy(started, fakeBBS.startedRunOnces)

	return started
}

func (fakeBBS *FakeExecutorBBS) SetStartRunOnceErr(err error) {
	fakeBBS.Lock()
	defer fakeBBS.Unlock()

	fakeBBS.startRunOnceErr = err
}

func (fakeBBS *FakeExecutorBBS) CompleteRunOnce(runOnce *models.RunOnce, failed bool, failureReason string, result string) error {
	fakeBBS.RLock()
	err := fakeBBS.completeRunOnceErr
	fakeBBS.RUnlock()

	if err != nil {
		return err
	}

	runOnce.Failed = failed
	runOnce.FailureReason = failureReason
	runOnce.Result = result

	fakeBBS.Lock()
	fakeBBS.completedRunOnces = append(fakeBBS.completedRunOnces, runOnce)
	fakeBBS.Unlock()

	return nil
}

func (fakeBBS *FakeExecutorBBS) CompletedRunOnces() []*models.RunOnce {
	fakeBBS.RLock()
	defer fakeBBS.RUnlock()

	completed := make([]*models.RunOnce, len(fakeBBS.completedRunOnces))
	copy(completed, fakeBBS.completedRunOnces)

	return completed
}

func (fakeBBS *FakeExecutorBBS) SetCompleteRunOnceErr(err error) {
	fakeBBS.Lock()
	defer fakeBBS.Unlock()

	fakeBBS.completeRunOnceErr = err
}

func (fakeBBS *FakeExecutorBBS) ConvergeRunOnce(timeToClaim time.Duration) {
	fakeBBS.Lock()
	defer fakeBBS.Unlock()

	fakeBBS.convergeTimeToClaimRunOnces = timeToClaim
	fakeBBS.callsToConverge++
}

func (fakeBBS *FakeExecutorBBS) MaintainConvergeLock(interval time.Duration, executorID string) (<-chan bool, chan<- chan bool, error) {
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

func (fakeBBS *FakeExecutorBBS) SetMaintainConvergeLockError(err error) {
	fakeBBS.Lock()
	defer fakeBBS.Unlock()

	fakeBBS.maintainConvergeLockError = err
}

func (fakeBBS *FakeExecutorBBS) Stop() {
	fakeBBS.RLock()
	presence := fakeBBS.maintainingPresencePresence
	stopConverge := fakeBBS.maintainConvergeStopChannel
	fakeBBS.RUnlock()

	if presence != nil {
		presence.Remove()
	}

	if stopConverge != nil {
		stopConverge <- nil
	}
}

func (fakeBBS *FakeExecutorBBS) ConvergeTimeToClaimRunOnces() time.Duration {
	fakeBBS.RLock()
	defer fakeBBS.RUnlock()

	return fakeBBS.convergeTimeToClaimRunOnces
}

func (fakeBBS *FakeExecutorBBS) CallsToConverge() int {
	fakeBBS.RLock()
	defer fakeBBS.RUnlock()

	return fakeBBS.callsToConverge
}
