package fake_bbs

import (
	"sync"

	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

type FakeAppManagerBBS struct {
	FileServerGetter

	lrpStartAuctions   []models.LRPStartAuction
	LRPStartAuctionErr error

	stopLRPInstances   []models.StopLRPInstance
	StopLRPInstanceErr error

	desiredLRPs  []models.DesiredLRP
	DesireLRPErr error

	removeDesiredLRPProcessGuids    []string
	removeDesiredLRPProcessGuidsErr error

	ActualLRPs    []models.ActualLRP
	ActualLRPsErr error

	sync.RWMutex
}

func NewFakeAppManagerBBS() *FakeAppManagerBBS {
	return &FakeAppManagerBBS{}
}

func (fakeBBS *FakeAppManagerBBS) DesireLRP(lrp models.DesiredLRP) error {
	fakeBBS.Lock()
	defer fakeBBS.Unlock()

	fakeBBS.desiredLRPs = append(fakeBBS.desiredLRPs, lrp)
	return fakeBBS.DesireLRPErr
}

func (fakeBBS *FakeAppManagerBBS) DesiredLRPs() []models.DesiredLRP {
	fakeBBS.RLock()
	defer fakeBBS.RUnlock()
	return fakeBBS.desiredLRPs
}

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

func (fakeBBS *FakeAppManagerBBS) RequestStopLRPInstance(lrp models.StopLRPInstance) error {
	fakeBBS.Lock()
	defer fakeBBS.Unlock()
	fakeBBS.stopLRPInstances = append(fakeBBS.stopLRPInstances, lrp)
	return fakeBBS.StopLRPInstanceErr
}

func (fakeBBS *FakeAppManagerBBS) GetStopLRPInstances() []models.StopLRPInstance {
	fakeBBS.RLock()
	defer fakeBBS.RUnlock()
	return fakeBBS.stopLRPInstances
}

func (fakeBBS *FakeAppManagerBBS) GetActualLRPsByProcessGuid(string) ([]models.ActualLRP, error) {
	fakeBBS.RLock()
	defer fakeBBS.RUnlock()
	return fakeBBS.ActualLRPs, fakeBBS.ActualLRPsErr
}

func (fakeBBS *FakeAppManagerBBS) RemoveDesiredLRPByProcessGuid(processGuid string) error {
	fakeBBS.Lock()
	defer fakeBBS.Unlock()
	fakeBBS.removeDesiredLRPProcessGuids = append(fakeBBS.removeDesiredLRPProcessGuids, processGuid)
	return fakeBBS.removeDesiredLRPProcessGuidsErr
}

func (fakeBBS *FakeAppManagerBBS) GetRemovedDesiredLRPProcessGuids() []string {
	fakeBBS.RLock()
	defer fakeBBS.RUnlock()
	return fakeBBS.removeDesiredLRPProcessGuids
}
