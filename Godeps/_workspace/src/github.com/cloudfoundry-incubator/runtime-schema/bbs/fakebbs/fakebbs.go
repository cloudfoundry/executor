package fakebbs

import (
	"github.com/cloudfoundry-incubator/runtime-schema/models"

	"time"
)

type FakeExecutorBBS struct {
	CallsToConverge int
	LockIsGrabbable bool
	ErrorOnGrabLock error

	MaintainingPresenceHeartbeatInterval uint64
	MaintainingPresenceExecutorID        string
	MaintainingPresenceStopChannel       chan bool
	MaintainingPresenceErrorChannel      chan error
	MaintainingPresenceError             error

	ClaimedRunOnce  models.RunOnce
	ClaimRunOnceErr error

	StartedRunOnce  models.RunOnce
	StartRunOnceErr error

	CompletedRunOnce   models.RunOnce
	CompleteRunOnceErr error
}

func NewFakeExecutorBBS() *FakeExecutorBBS {
	return &FakeExecutorBBS{}
}

func (fakeBBS *FakeExecutorBBS) MaintainExecutorPresence(heartbeatIntervalInSeconds uint64, executorID string) (chan bool, chan error, error) {
	fakeBBS.MaintainingPresenceHeartbeatInterval = heartbeatIntervalInSeconds
	fakeBBS.MaintainingPresenceExecutorID = executorID
	fakeBBS.MaintainingPresenceStopChannel = make(chan bool)
	fakeBBS.MaintainingPresenceErrorChannel = make(chan error)

	return fakeBBS.MaintainingPresenceStopChannel, fakeBBS.MaintainingPresenceErrorChannel, fakeBBS.MaintainingPresenceError
}

func (fakeBBS *FakeExecutorBBS) WatchForDesiredRunOnce() (<-chan models.RunOnce, chan<- bool, <-chan error) {
	return nil, nil, nil
}

func (fakeBBS *FakeExecutorBBS) ClaimRunOnce(runOnce models.RunOnce) error {
	fakeBBS.ClaimedRunOnce = runOnce
	return fakeBBS.ClaimRunOnceErr
}

func (fakeBBS *FakeExecutorBBS) StartRunOnce(runOnce models.RunOnce) error {
	fakeBBS.StartedRunOnce = runOnce
	return fakeBBS.StartRunOnceErr
}

func (fakeBBS *FakeExecutorBBS) CompleteRunOnce(runOnce models.RunOnce) error {
	fakeBBS.CompletedRunOnce = runOnce
	return fakeBBS.CompleteRunOnceErr
}

func (fakeBBS *FakeExecutorBBS) ConvergeRunOnce() {
	fakeBBS.CallsToConverge++
}

func (fakeBBS *FakeExecutorBBS) GrabRunOnceLock(time.Duration) (bool, error) {
	return fakeBBS.LockIsGrabbable, fakeBBS.ErrorOnGrabLock
}
