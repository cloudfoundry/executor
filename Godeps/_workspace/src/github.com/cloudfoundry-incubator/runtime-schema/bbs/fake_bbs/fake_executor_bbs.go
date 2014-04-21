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

	claimedTasks []*models.Task
	claimTaskErr error

	startedTasks []*models.Task
	startTaskErr error

	completedTasks           []*models.Task
	completeTaskErr          error
	convergeTimeToClaimTasks time.Duration

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

func (fakeBBS *FakeExecutorBBS) WatchForDesiredTask() (<-chan *models.Task, chan<- bool, <-chan error) {
	return nil, nil, nil
}

func (fakeBBS *FakeExecutorBBS) ClaimTask(task *models.Task, executorID string) error {
	task.ExecutorID = executorID

	fakeBBS.RLock()
	err := fakeBBS.claimTaskErr
	fakeBBS.RUnlock()

	if err != nil {
		return err
	}

	fakeBBS.Lock()
	fakeBBS.claimedTasks = append(fakeBBS.claimedTasks, task)
	fakeBBS.Unlock()

	return nil
}

func (fakeBBS *FakeExecutorBBS) ClaimedTasks() []*models.Task {
	fakeBBS.RLock()
	defer fakeBBS.RUnlock()

	claimed := make([]*models.Task, len(fakeBBS.claimedTasks))
	copy(claimed, fakeBBS.claimedTasks)

	return claimed
}

func (fakeBBS *FakeExecutorBBS) SetClaimTaskErr(err error) {
	fakeBBS.Lock()
	defer fakeBBS.Unlock()

	fakeBBS.claimTaskErr = err
}

func (fakeBBS *FakeExecutorBBS) StartTask(task *models.Task, containerHandle string) error {
	fakeBBS.RLock()
	err := fakeBBS.startTaskErr
	fakeBBS.RUnlock()

	if err != nil {
		return err
	}

	task.ContainerHandle = containerHandle

	fakeBBS.Lock()
	fakeBBS.startedTasks = append(fakeBBS.startedTasks, task)
	fakeBBS.Unlock()

	return nil
}

func (fakeBBS *FakeExecutorBBS) StartedTasks() []*models.Task {
	fakeBBS.RLock()
	defer fakeBBS.RUnlock()

	started := make([]*models.Task, len(fakeBBS.startedTasks))
	copy(started, fakeBBS.startedTasks)

	return started
}

func (fakeBBS *FakeExecutorBBS) SetStartTaskErr(err error) {
	fakeBBS.Lock()
	defer fakeBBS.Unlock()

	fakeBBS.startTaskErr = err
}

func (fakeBBS *FakeExecutorBBS) CompleteTask(task *models.Task, failed bool, failureReason string, result string) error {
	fakeBBS.RLock()
	err := fakeBBS.completeTaskErr
	fakeBBS.RUnlock()

	if err != nil {
		return err
	}

	task.Failed = failed
	task.FailureReason = failureReason
	task.Result = result

	fakeBBS.Lock()
	fakeBBS.completedTasks = append(fakeBBS.completedTasks, task)
	fakeBBS.Unlock()

	return nil
}

func (fakeBBS *FakeExecutorBBS) CompletedTasks() []*models.Task {
	fakeBBS.RLock()
	defer fakeBBS.RUnlock()

	completed := make([]*models.Task, len(fakeBBS.completedTasks))
	copy(completed, fakeBBS.completedTasks)

	return completed
}

func (fakeBBS *FakeExecutorBBS) SetCompleteTaskErr(err error) {
	fakeBBS.Lock()
	defer fakeBBS.Unlock()

	fakeBBS.completeTaskErr = err
}

func (fakeBBS *FakeExecutorBBS) ConvergeTask(timeToClaim time.Duration) {
	fakeBBS.Lock()
	defer fakeBBS.Unlock()

	fakeBBS.convergeTimeToClaimTasks = timeToClaim
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

func (fakeBBS *FakeExecutorBBS) ConvergeTimeToClaimTasks() time.Duration {
	fakeBBS.RLock()
	defer fakeBBS.RUnlock()

	return fakeBBS.convergeTimeToClaimTasks
}

func (fakeBBS *FakeExecutorBBS) CallsToConverge() int {
	fakeBBS.RLock()
	defer fakeBBS.RUnlock()

	return fakeBBS.callsToConverge
}
