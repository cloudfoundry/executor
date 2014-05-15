package fake_bbs

import (
	"sync"
	"time"

	"github.com/cloudfoundry-incubator/runtime-schema/bbs/services_bbs"
)

type FakeExecutorBBS struct {
	MaintainExecutorPresenceInputs struct {
		HeartbeatInterval chan time.Duration
		ExecutorID        chan string
	}
	MaintainExecutorPresenceOutputs struct {
		Presence *FakePresence
		Error    error
	}

	sync.RWMutex
}

func NewFakeExecutorBBS() *FakeExecutorBBS {
	fakeBBS := &FakeExecutorBBS{}
	fakeBBS.MaintainExecutorPresenceInputs.HeartbeatInterval = make(chan time.Duration, 1)
	fakeBBS.MaintainExecutorPresenceInputs.ExecutorID = make(chan string, 1)
	return fakeBBS
}

func (fakeBBS *FakeExecutorBBS) MaintainExecutorPresence(heartbeatInterval time.Duration, executorID string) (services_bbs.Presence, <-chan bool, error) {
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

func (fakeBBS *FakeExecutorBBS) Stop() {
	fakeBBS.RLock()
	presence := fakeBBS.MaintainExecutorPresenceOutputs.Presence
	fakeBBS.RUnlock()

	if presence != nil {
		presence.Remove()
	}
}
