package lrp_bbs

import (
	"fmt"

	"github.com/cloudfoundry-incubator/runtime-schema/bbs/shared"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/storeadapter"
)

func (bbs *LongRunningProcessBBS) DesireTransitionalLongRunningProcess(lrp models.TransitionalLongRunningProcess) error {
	return shared.RetryIndefinitelyOnStoreTimeout(func() error {
		lrp.State = models.TransitionalLRPStateDesired
		return bbs.store.SetMulti([]storeadapter.StoreNode{
			{
				Key:   shared.TransitionalLongRunningProcessSchemaPath(lrp),
				Value: lrp.ToJSON(),
			},
		})
	})
}

func (self *LongRunningProcessBBS) StartTransitionalLongRunningProcess(lrp models.TransitionalLongRunningProcess) error {
	originalValue := lrp.ToJSON()

	lrp.State = models.TransitionalLRPStateRunning
	changedValue := lrp.ToJSON()

	return shared.RetryIndefinitelyOnStoreTimeout(func() error {
		return self.store.CompareAndSwap(storeadapter.StoreNode{
			Key:   shared.TransitionalLongRunningProcessSchemaPath(lrp),
			Value: originalValue,
		}, storeadapter.StoreNode{
			Key:   shared.TransitionalLongRunningProcessSchemaPath(lrp),
			Value: changedValue,
		})
	})
}

func (bbs *LongRunningProcessBBS) GetAllTransitionalLongRunningProcesses() ([]models.TransitionalLongRunningProcess, error) {
	lrps := []models.TransitionalLongRunningProcess{}

	node, err := bbs.store.ListRecursively(shared.LongRunningProcessSchemaRoot)
	if err == storeadapter.ErrorKeyNotFound {
		return lrps, nil
	}

	if err != nil {
		return lrps, err
	}

	for _, node := range node.ChildNodes {
		lrp, err := models.NewTransitionalLongRunningProcessFromJSON(node.Value)
		if err != nil {
			return lrps, fmt.Errorf("cannot parse lrp JSON for key %s: %s", node.Key, err.Error())
		} else {
			lrps = append(lrps, lrp)
		}
	}

	return lrps, nil
}

func (self *LongRunningProcessBBS) WatchForDesiredTransitionalLongRunningProcess() (<-chan models.TransitionalLongRunningProcess, chan<- bool, <-chan error) {
	return watchForLrpModificationsOnState(self.store, models.TransitionalLRPStateDesired)
}

func watchForLrpModificationsOnState(store storeadapter.StoreAdapter, state models.TransitionalLRPState) (<-chan models.TransitionalLongRunningProcess, chan<- bool, <-chan error) {
	lrps := make(chan models.TransitionalLongRunningProcess)
	stopOuter := make(chan bool)
	errsOuter := make(chan error)

	events, stopInner, errsInner := store.Watch(shared.LongRunningProcessSchemaRoot)

	go func() {
		defer close(lrps)
		defer close(errsOuter)

		for {
			select {
			case <-stopOuter:
				close(stopInner)
				return

			case event, ok := <-events:
				if !ok {
					return
				}
				lrp, err := models.NewTransitionalLongRunningProcessFromJSON(event.Node.Value)
				if err != nil {
					continue
				}

				if lrp.State == state {
					lrps <- lrp
				}

			case err, ok := <-errsInner:
				if ok {
					errsOuter <- err
				}
				return
			}
		}
	}()

	return lrps, stopOuter, errsOuter
}
