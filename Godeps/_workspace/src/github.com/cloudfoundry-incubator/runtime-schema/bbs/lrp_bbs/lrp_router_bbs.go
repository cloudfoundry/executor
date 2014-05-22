package lrp_bbs

import (
	"fmt"

	"github.com/cloudfoundry-incubator/runtime-schema/bbs/shared"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/storeadapter"
)

func (self *LongRunningProcessBBS) WatchForDesiredLongRunningProcesses() (<-chan models.DesiredLRP, chan<- bool, <-chan error) {
	return watchForDesiredLRPs(self.store)
}

func (self *LongRunningProcessBBS) WatchForActualLongRunningProcesses() (<-chan models.LRP, chan<- bool, <-chan error) {
	return watchForActualLRPs(self.store)
}

func (bbs *LongRunningProcessBBS) GetAllDesiredLongRunningProcesses() ([]models.DesiredLRP, error) {
	lrps := []models.DesiredLRP{}

	node, err := bbs.store.ListRecursively(shared.DesiredLRPSchemaRoot)
	if err == storeadapter.ErrorKeyNotFound {
		return lrps, nil
	}

	if err != nil {
		return lrps, err
	}

	for _, node := range node.ChildNodes {
		lrp, err := models.NewDesiredLRPFromJSON(node.Value)
		if err != nil {
			return lrps, fmt.Errorf("cannot parse lrp JSON for key %s: %s", node.Key, err.Error())
		} else {
			lrps = append(lrps, lrp)
		}
	}

	return lrps, nil
}

func (bbs *LongRunningProcessBBS) GetAllActualLongRunningProcesses() ([]models.LRP, error) {
	lrps := []models.LRP{}

	node, err := bbs.store.ListRecursively(shared.ActualLRPSchemaRoot)
	if err == storeadapter.ErrorKeyNotFound {
		return lrps, nil
	}

	if err != nil {
		return lrps, err
	}

	for _, node := range node.ChildNodes {
		for _, indexNode := range node.ChildNodes {
			for _, instanceNode := range indexNode.ChildNodes {
				lrp, err := models.NewLRPFromJSON(instanceNode.Value)
				if err != nil {
					return lrps, fmt.Errorf("cannot parse lrp JSON for key %s: %s", instanceNode.Key, err.Error())
				} else {
					lrps = append(lrps, lrp)
				}
			}
		}
	}

	return lrps, nil
}

func watchForDesiredLRPs(store storeadapter.StoreAdapter) (<-chan models.DesiredLRP, chan<- bool, <-chan error) {
	lrps := make(chan models.DesiredLRP)
	stopOuter := make(chan bool)
	errsOuter := make(chan error)

	events, stopInner, errsInner := store.Watch(shared.DesiredLRPSchemaRoot)

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

				lrp, err := models.NewDesiredLRPFromJSON(event.Node.Value)
				if err != nil {
					continue
				}

				lrps <- lrp

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

func watchForActualLRPs(store storeadapter.StoreAdapter) (<-chan models.LRP, chan<- bool, <-chan error) {
	lrps := make(chan models.LRP)
	stopOuter := make(chan bool)
	errsOuter := make(chan error)

	events, stopInner, errsInner := store.Watch(shared.ActualLRPSchemaRoot)

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

				lrp, err := models.NewLRPFromJSON(event.Node.Value)
				if err != nil {
					continue
				}

				lrps <- lrp

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
