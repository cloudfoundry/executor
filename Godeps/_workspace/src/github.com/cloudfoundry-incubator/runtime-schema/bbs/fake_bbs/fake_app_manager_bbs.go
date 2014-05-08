package fake_bbs

import (
	"sync"

	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

type FakeAppManagerBBS struct {
	desiredLrps  []models.TransitionalLongRunningProcess
	DesireLrpErr error
	sync.RWMutex
}

func NewFakeAppManagerBBS() *FakeAppManagerBBS {
	return &FakeAppManagerBBS{}
}

func (fakeBBS *FakeAppManagerBBS) DesireTransitionalLongRunningProcess(lrp models.TransitionalLongRunningProcess) error {
	fakeBBS.Lock()
	defer fakeBBS.Unlock()
	fakeBBS.desiredLrps = append(fakeBBS.desiredLrps, lrp)
	return fakeBBS.DesireLrpErr
}

func (fakeBBS *FakeAppManagerBBS) DesiredLrps() []models.TransitionalLongRunningProcess {
	fakeBBS.RLock()
	defer fakeBBS.RUnlock()
	return fakeBBS.desiredLrps
}
