package fake_bbs

import (
	"sync"
	"time"

	"github.com/cloudfoundry-incubator/runtime-schema/bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

type FakeExecutorBBS struct {
	callsToConverge int

	desiredTaskChan     chan models.Task
	desiredTaskStopChan chan bool
	desiredTaskErrChan  chan error

	desiredLrpChan chan models.TransitionalLongRunningProcess

	maintainConvergeInterval      time.Duration
	maintainConvergeExecutorID    string
	maintainConvergeStatusChannel <-chan bool
	maintainConvergeStopChannel   chan<- chan bool
	maintainConvergeLockError     error

	MaintainExecutorPresenceInputs struct {
		HeartbeatInterval chan time.Duration
		ExecutorID        chan string
	}
	MaintainExecutorPresenceOutputs struct {
		Presence *FakePresence
		Error    error
	}

	claimedTasks []models.Task
	claimTaskErr error

	startedTasks []models.Task
	startTaskErr error

	completedTasks           []models.Task
	completeTaskErr          error
	convergeTimeToClaimTasks time.Duration

	startedLrps []models.TransitionalLongRunningProcess
	startLrpErr error

	sync.RWMutex
}

func NewFakeExecutorBBS() *FakeExecutorBBS {
	fakeBBS := &FakeExecutorBBS{}
	fakeBBS.MaintainExecutorPresenceInputs.HeartbeatInterval = make(chan time.Duration, 1)
	fakeBBS.MaintainExecutorPresenceInputs.ExecutorID = make(chan string, 1)
	fakeBBS.desiredTaskChan = make(chan models.Task, 1)
	fakeBBS.desiredTaskStopChan = make(chan bool)
	fakeBBS.desiredTaskErrChan = make(chan error)
	fakeBBS.desiredLrpChan = make(chan models.TransitionalLongRunningProcess, 1)
	return fakeBBS
}

func (fakeBBS *FakeExecutorBBS) MaintainExecutorPresence(heartbeatInterval time.Duration, executorID string) (bbs.Presence, <-chan bool, error) {
	fakeBBS.MaintainExecutorPresenceInputs.HeartbeatInterval <- heartbeatInterval
	fakeBBS.MaintainExecutorPresenceInputs.ExecutorID <- executorID

	presence := fakeBBS.MaintainExecutorPresenceOutputs.Presence

	if presence == nil {
		presence = &FakePresence{
			MaintainStatus: true,
		}
	}

	status, _ := presence.Maintain(heartbeatInterval)

	return presence, status, fakeBBS.MaintainExecutorPresenceOutputs.Error
}

func (fakeBBS *FakeExecutorBBS) GetMaintainExecutorPresenceHeartbeatInterval() time.Duration {
	return <-fakeBBS.MaintainExecutorPresenceInputs.HeartbeatInterval
}

func (fakeBBS *FakeExecutorBBS) GetMaintainExecutorPresenceId() string {
	return <-fakeBBS.MaintainExecutorPresenceInputs.ExecutorID
}

func (fakeBBS *FakeExecutorBBS) WatchForDesiredTask() (<-chan models.Task, chan<- bool, <-chan error) {
	return fakeBBS.desiredTaskChan, fakeBBS.desiredTaskStopChan, fakeBBS.desiredTaskErrChan
}

func (fakeBBS *FakeExecutorBBS) WatchForDesiredTransitionalLongRunningProcess() (<-chan models.TransitionalLongRunningProcess, chan<- bool, <-chan error) {
	return fakeBBS.desiredLrpChan, nil, nil
}

func (fakeBBS *FakeExecutorBBS) EmitDesiredTask(task models.Task) {
	fakeBBS.desiredTaskChan <- task
}

func (fakeBBS *FakeExecutorBBS) ClaimTask(task models.Task, executorID string) (models.Task, error) {
	task.ExecutorID = executorID

	fakeBBS.RLock()
	err := fakeBBS.claimTaskErr
	fakeBBS.RUnlock()

	if err != nil {
		return task, err
	}

	fakeBBS.Lock()
	fakeBBS.claimedTasks = append(fakeBBS.claimedTasks, task)
	fakeBBS.Unlock()

	return task, nil
}

func (fakeBBS *FakeExecutorBBS) ClaimedTasks() []models.Task {
	fakeBBS.RLock()
	defer fakeBBS.RUnlock()

	claimed := make([]models.Task, len(fakeBBS.claimedTasks))
	copy(claimed, fakeBBS.claimedTasks)

	return claimed
}

func (fakeBBS *FakeExecutorBBS) SetClaimTaskErr(err error) {
	fakeBBS.Lock()
	defer fakeBBS.Unlock()

	fakeBBS.claimTaskErr = err
}

func (fakeBBS *FakeExecutorBBS) StartTask(task models.Task, containerHandle string) (models.Task, error) {
	fakeBBS.RLock()
	err := fakeBBS.startTaskErr
	fakeBBS.RUnlock()

	if err != nil {
		return task, err
	}

	task.ContainerHandle = containerHandle

	fakeBBS.Lock()
	fakeBBS.startedTasks = append(fakeBBS.startedTasks, task)
	fakeBBS.Unlock()

	return task, nil
}

func (fakeBBS *FakeExecutorBBS) StartTransitionalLongRunningProcess(lrp models.TransitionalLongRunningProcess) error {
	fakeBBS.RLock()
	err := fakeBBS.startLrpErr
	fakeBBS.RUnlock()

	if err != nil {
		return err
	}

	fakeBBS.Lock()
	fakeBBS.startedLrps = append(fakeBBS.startedLrps, lrp)
	fakeBBS.Unlock()

	return nil
}

func (fakeBBS *FakeExecutorBBS) StartedTasks() []models.Task {
	fakeBBS.RLock()
	defer fakeBBS.RUnlock()

	started := make([]models.Task, len(fakeBBS.startedTasks))
	copy(started, fakeBBS.startedTasks)

	return started
}

func (fakeBBS *FakeExecutorBBS) SetStartTaskErr(err error) {
	fakeBBS.Lock()
	defer fakeBBS.Unlock()

	fakeBBS.startTaskErr = err
}

func (fakeBBS *FakeExecutorBBS) CompleteTask(task models.Task, failed bool, failureReason string, result string) (models.Task, error) {
	fakeBBS.RLock()
	err := fakeBBS.completeTaskErr
	fakeBBS.RUnlock()

	if err != nil {
		return task, err
	}

	task.Failed = failed
	task.FailureReason = failureReason
	task.Result = result

	fakeBBS.Lock()
	fakeBBS.completedTasks = append(fakeBBS.completedTasks, task)
	fakeBBS.Unlock()

	return task, nil
}

func (fakeBBS *FakeExecutorBBS) CompletedTasks() []models.Task {
	fakeBBS.RLock()
	defer fakeBBS.RUnlock()

	completed := make([]models.Task, len(fakeBBS.completedTasks))
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
	presence := fakeBBS.MaintainExecutorPresenceOutputs.Presence
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
