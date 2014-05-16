package fake_bbs

import (
	"sync"

	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

type FakeAppManagerBBS struct {
	desiredLrps        []models.TransitionalLongRunningProcess
	lrpStartAuctions   []models.LRPStartAuction
	DesireLrpErr       error
	LRPStartAuctionErr error
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

///////////////////////

func (fakeBBS *FakeAppManagerBBS) RequestLRPStartAuction(lrp models.LRPStartAuction) error {
	fakeBBS.Lock()
	defer fakeBBS.Unlock()
	fakeBBS.lrpStartAuctions = append(fakeBBS.lrpStartAuctions, lrp)
	return fakeBBS.LRPStartAuctionErr
}

func (fakeBBS *FakeAppManagerBBS) GetLRPStartAuctions() []models.LRPStartAuction {
	fakeBBS.RLock()
	defer fakeBBS.RUnlock()
	return fakeBBS.lrpStartAuctions
}
