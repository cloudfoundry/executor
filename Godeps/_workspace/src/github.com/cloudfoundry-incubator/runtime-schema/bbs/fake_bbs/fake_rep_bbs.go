package fake_bbs

import (
	"sync"
	"time"

	"github.com/cloudfoundry-incubator/runtime-schema/bbs/services_bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

type FakeRepBBS struct {
	desiredTaskChan     chan models.Task
	desiredTaskStopChan chan bool
	desiredTaskErrChan  chan error

	stopLRPInstanceChan     chan models.StopLRPInstance
	stopLRPInstanceStopChan chan bool
	stopLRPInstanceErrChan  chan error

	resolvedStopLRPInstances  []models.StopLRPInstance
	resolveStopLRPInstanceErr error

	claimedTasks []models.Task
	claimTaskErr error

	startedTasks []models.Task
	startTaskErr error

	completedTasks           []models.Task
	completeTaskErr          error
	convergeTimeToClaimTasks time.Duration

	runningLrps            []models.ActualLRP
	runningLrpsExecutorIDs []string
	runningLrpErr          error

	startingLrps            []models.ActualLRP
	startingLrpsExecutorIDs []string
	startingLrpErr          error

	removedLrps []models.ActualLRP

	MaintainRepPresenceInput struct {
		HeartbeatInterval time.Duration
		RepPresence       models.RepPresence
	}
	MaintainRepPresenceOutput struct {
		Presence *FakePresence
		Error    error
	}

	sync.RWMutex
}

func NewFakeRepBBS() *FakeRepBBS {
	fakeBBS := &FakeRepBBS{}
	fakeBBS.desiredTaskChan = make(chan models.Task, 1)
	fakeBBS.desiredTaskStopChan = make(chan bool)
	fakeBBS.desiredTaskErrChan = make(chan error)
	fakeBBS.stopLRPInstanceChan = make(chan models.StopLRPInstance)
	fakeBBS.stopLRPInstanceStopChan = make(chan bool)
	fakeBBS.stopLRPInstanceErrChan = make(chan error)

	return fakeBBS
}

func (fakeBBS *FakeRepBBS) WatchForDesiredTask() (<-chan models.Task, chan<- bool, <-chan error) {
	return fakeBBS.desiredTaskChan, fakeBBS.desiredTaskStopChan, fakeBBS.desiredTaskErrChan
}

func (fakeBBS *FakeRepBBS) WatchForDesiredTaskError(err error) {
	fakeBBS.desiredTaskErrChan <- err
}

func (fakeBBS *FakeRepBBS) EmitDesiredTask(task models.Task) {
	fakeBBS.desiredTaskChan <- task
}

func (fakeBBS *FakeRepBBS) ClaimTask(task models.Task, executorID string) (models.Task, error) {
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

func (fakeBBS *FakeRepBBS) ClaimedTasks() []models.Task {
	fakeBBS.RLock()
	defer fakeBBS.RUnlock()

	claimed := make([]models.Task, len(fakeBBS.claimedTasks))
	copy(claimed, fakeBBS.claimedTasks)

	return claimed
}

func (fakeBBS *FakeRepBBS) SetClaimTaskErr(err error) {
	fakeBBS.Lock()
	defer fakeBBS.Unlock()

	fakeBBS.claimTaskErr = err
}

func (fakeBBS *FakeRepBBS) StartTask(task models.Task, containerHandle string) (models.Task, error) {
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

func (fakeBBS *FakeRepBBS) ReportActualLRPAsStarting(lrp models.ActualLRP, executorID string) error {
	fakeBBS.RLock()
	err := fakeBBS.startingLrpErr
	fakeBBS.RUnlock()

	if err != nil {
		return err
	}

	fakeBBS.Lock()
	fakeBBS.startingLrps = append(fakeBBS.startingLrps, lrp)
	fakeBBS.startingLrpsExecutorIDs = append(fakeBBS.startingLrpsExecutorIDs, executorID)
	fakeBBS.Unlock()

	return nil
}

func (fakeBBS *FakeRepBBS) StartingLRPs() []models.ActualLRP {
	fakeBBS.RLock()
	defer fakeBBS.RUnlock()

	running := make([]models.ActualLRP, len(fakeBBS.startingLrps))
	copy(running, fakeBBS.startingLrps)

	return running
}

func (fakeBBS *FakeRepBBS) StartingLRPExecutorIDs() []string {
	fakeBBS.RLock()
	defer fakeBBS.RUnlock()

	return fakeBBS.startingLrpsExecutorIDs
}

func (fakeBBS *FakeRepBBS) SetStartingError(err error) {
	fakeBBS.Lock()
	defer fakeBBS.Unlock()

	fakeBBS.startingLrpErr = err
}

func (fakeBBS *FakeRepBBS) ReportActualLRPAsRunning(lrp models.ActualLRP, executorID string) error {
	fakeBBS.RLock()
	err := fakeBBS.runningLrpErr
	fakeBBS.RUnlock()

	if err != nil {
		return err
	}

	fakeBBS.Lock()
	fakeBBS.runningLrps = append(fakeBBS.runningLrps, lrp)
	fakeBBS.runningLrpsExecutorIDs = append(fakeBBS.startingLrpsExecutorIDs, executorID)
	fakeBBS.Unlock()

	return nil
}

func (fakeBBS *FakeRepBBS) RunningLRPs() []models.ActualLRP {
	fakeBBS.RLock()
	defer fakeBBS.RUnlock()

	running := make([]models.ActualLRP, len(fakeBBS.runningLrps))
	copy(running, fakeBBS.runningLrps)

	return running
}

func (fakeBBS *FakeRepBBS) RunningLRPsExecutorIDs() []string {
	fakeBBS.RLock()
	defer fakeBBS.RUnlock()

	return fakeBBS.runningLrpsExecutorIDs
}

func (fakeBBS *FakeRepBBS) SetRunningError(err error) {
	fakeBBS.Lock()
	defer fakeBBS.Unlock()

	fakeBBS.runningLrpErr = err
}

func (fakeBBS *FakeRepBBS) RemoveActualLRP(lrp models.ActualLRP) error {
	fakeBBS.Lock()
	fakeBBS.removedLrps = append(fakeBBS.removedLrps, lrp)
	fakeBBS.Unlock()

	return nil
}

func (fakeBBS *FakeRepBBS) RemovedLRPs() []models.ActualLRP {
	fakeBBS.RLock()
	defer fakeBBS.RUnlock()

	removed := make([]models.ActualLRP, len(fakeBBS.removedLrps))
	copy(removed, fakeBBS.removedLrps)

	return removed
}

func (fakeBBS *FakeRepBBS) WatchForStopLRPInstance() (<-chan models.StopLRPInstance, chan<- bool, <-chan error) {
	return fakeBBS.stopLRPInstanceChan, fakeBBS.stopLRPInstanceStopChan, fakeBBS.stopLRPInstanceErrChan
}

func (fakeBBS *FakeRepBBS) WatchForStopLRPInstanceError(err error) {
	fakeBBS.stopLRPInstanceErrChan <- err
}

func (fakeBBS *FakeRepBBS) EmitStopLRPInstance(stopInstance models.StopLRPInstance) {
	fakeBBS.stopLRPInstanceChan <- stopInstance
}

func (fakeBBS *FakeRepBBS) ResolveStopLRPInstance(stopInstance models.StopLRPInstance) error {
	fakeBBS.Lock()
	fakeBBS.resolvedStopLRPInstances = append(fakeBBS.resolvedStopLRPInstances, stopInstance)
	fakeBBS.Unlock()

	return fakeBBS.resolveStopLRPInstanceErr
}

func (fakeBBS *FakeRepBBS) SetResolveStopLRPInstanceError(err error) {
	fakeBBS.RLock()
	defer fakeBBS.RUnlock()

	fakeBBS.resolveStopLRPInstanceErr = err
}

func (fakeBBS *FakeRepBBS) ResolvedStopLRPInstances() []models.StopLRPInstance {
	fakeBBS.RLock()
	defer fakeBBS.RUnlock()

	removed := make([]models.StopLRPInstance, len(fakeBBS.resolvedStopLRPInstances))
	copy(removed, fakeBBS.resolvedStopLRPInstances)

	return removed
}

func (fakeBBS *FakeRepBBS) StartedTasks() []models.Task {
	fakeBBS.RLock()
	defer fakeBBS.RUnlock()

	started := make([]models.Task, len(fakeBBS.startedTasks))
	copy(started, fakeBBS.startedTasks)

	return started
}

func (fakeBBS *FakeRepBBS) SetStartTaskErr(err error) {
	fakeBBS.Lock()
	defer fakeBBS.Unlock()

	fakeBBS.startTaskErr = err
}

func (fakeBBS *FakeRepBBS) CompleteTask(task models.Task, failed bool, failureReason string, result string) (models.Task, error) {
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

func (fakeBBS *FakeRepBBS) CompletedTasks() []models.Task {
	fakeBBS.RLock()
	defer fakeBBS.RUnlock()

	completed := make([]models.Task, len(fakeBBS.completedTasks))
	copy(completed, fakeBBS.completedTasks)

	return completed
}

func (fakeBBS *FakeRepBBS) SetCompleteTaskErr(err error) {
	fakeBBS.Lock()
	defer fakeBBS.Unlock()

	fakeBBS.completeTaskErr = err
}

func (fakeBBS *FakeRepBBS) MaintainRepPresence(heartbeatInterval time.Duration, repPresence models.RepPresence) (services_bbs.Presence, <-chan bool, error) {
	fakeBBS.Lock()
	fakeBBS.MaintainRepPresenceInput.HeartbeatInterval = heartbeatInterval
	fakeBBS.MaintainRepPresenceInput.RepPresence = repPresence
	fakeBBS.Unlock()

	presence := fakeBBS.MaintainRepPresenceOutput.Presence

	if presence == nil {
		presence = &FakePresence{
			MaintainStatus: true,
		}
	}

	status, _ := presence.Maintain(heartbeatInterval)

	return presence, status, fakeBBS.MaintainRepPresenceOutput.Error
}

func (fakeBBS *FakeRepBBS) GetMaintainRepPresenceHeartbeatInterval() time.Duration {
	fakeBBS.Lock()
	defer fakeBBS.Unlock()
	return fakeBBS.MaintainRepPresenceInput.HeartbeatInterval
}

func (fakeBBS *FakeRepBBS) GetMaintainRepPresence() models.RepPresence {
	fakeBBS.Lock()
	defer fakeBBS.Unlock()
	return fakeBBS.MaintainRepPresenceInput.RepPresence
}

func (fakeBBS *FakeRepBBS) Stop() {
	fakeBBS.RLock()
	presence := fakeBBS.MaintainRepPresenceOutput.Presence
	fakeBBS.RUnlock()

	if presence != nil {
		presence.Remove()
	}
}
