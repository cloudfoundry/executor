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

func (fakeBBS *FakeExecutorBBS) ClaimRunOnce(models.RunOnce) error {
	return nil
}

func (fakeBBS *FakeExecutorBBS) StartRunOnce(models.RunOnce) error {
	return nil
}

func (fakeBBS *FakeExecutorBBS) CompleteRunOnce(models.RunOnce) error {
	return nil
}

func (fakeBBS *FakeExecutorBBS) ConvergeRunOnce() {
	fakeBBS.CallsToConverge++
}

func (fakeBBS *FakeExecutorBBS) GrabRunOnceLock(time.Duration) (bool, error) {
	return fakeBBS.LockIsGrabbable, fakeBBS.ErrorOnGrabLock
}
