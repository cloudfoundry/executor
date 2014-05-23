package fake_bbs

import "github.com/cloudfoundry-incubator/runtime-schema/models"

type FakeLRPRouterBBS struct {
	DesiredLRPChangeChan chan models.DesiredLRPChange
	desiredLRPStopChan   chan bool
	desiredLRPErrChan    chan error

	ActualLRPChangeChan chan models.ActualLRPChange
	actualLRPStopChan   chan bool
	actualLRPErrChan    chan error

	AllDesiredLRPs []models.DesiredLRP
	AllActualLRPs  []models.LRP

	DesiredLRP models.DesiredLRP
	ActualLRPs []models.LRP

	WhenGettingRunningActualLRPs func() ([]models.LRP, error)
	WhenGettingAllDesiredLRPs    func() ([]models.DesiredLRP, error)
}

func NewFakeLRPRouterBBS() *FakeLRPRouterBBS {
	return &FakeLRPRouterBBS{
		DesiredLRPChangeChan: make(chan models.DesiredLRPChange, 1),
		desiredLRPStopChan:   make(chan bool),
		desiredLRPErrChan:    make(chan error),

		ActualLRPChangeChan: make(chan models.ActualLRPChange, 1),
		actualLRPStopChan:   make(chan bool),
		actualLRPErrChan:    make(chan error),
	}
}

func (fakeBBS *FakeLRPRouterBBS) WatchForDesiredLRPChanges() (<-chan models.DesiredLRPChange, chan<- bool, <-chan error) {
	return fakeBBS.DesiredLRPChangeChan, fakeBBS.desiredLRPStopChan, fakeBBS.desiredLRPErrChan
}

func (fakeBBS *FakeLRPRouterBBS) WatchForActualLRPChanges() (<-chan models.ActualLRPChange, chan<- bool, <-chan error) {
	return fakeBBS.ActualLRPChangeChan, fakeBBS.actualLRPStopChan, fakeBBS.actualLRPErrChan
}

func (fakeBBS *FakeLRPRouterBBS) GetAllDesiredLRPs() ([]models.DesiredLRP, error) {
	if fakeBBS.WhenGettingAllDesiredLRPs != nil {
		return fakeBBS.WhenGettingAllDesiredLRPs()
	}
	return fakeBBS.AllDesiredLRPs, nil
}

func (fakeBBS *FakeLRPRouterBBS) GetRunningActualLRPs() ([]models.LRP, error) {
	if fakeBBS.WhenGettingRunningActualLRPs != nil {
		return fakeBBS.WhenGettingRunningActualLRPs()
	}
	return fakeBBS.AllActualLRPs, nil
}

func (fakeBBS *FakeLRPRouterBBS) GetDesiredLRPByProcessGuid(processGuid string) (models.DesiredLRP, error) {
	return fakeBBS.DesiredLRP, nil
}

func (fakeBBS *FakeLRPRouterBBS) GetRunningActualLRPsByProcessGuid(processGuid string) ([]models.LRP, error) {
	return fakeBBS.ActualLRPs, nil
}
