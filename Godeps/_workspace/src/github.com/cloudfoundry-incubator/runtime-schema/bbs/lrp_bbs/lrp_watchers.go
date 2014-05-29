package lrp_bbs

import (
	"github.com/cloudfoundry-incubator/runtime-schema/bbs/shared"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/storeadapter"
)

func (self *LRPBBS) WatchForDesiredLRPChanges() (<-chan models.DesiredLRPChange, chan<- bool, <-chan error) {
	return watchForDesiredLRPChanges(self.store)
}

//XXXX
func (self *LRPBBS) WatchForActualLRPChanges() (<-chan models.ActualLRPChange, chan<- bool, <-chan error) {
	return watchForActualLRPs(self.store)
}

func watchForDesiredLRPChanges(store storeadapter.StoreAdapter) (<-chan models.DesiredLRPChange, chan<- bool, <-chan error) {
	changes := make(chan models.DesiredLRPChange)
	stopOuter := make(chan bool)
	errsOuter := make(chan error)

	events, stopInner, errsInner := store.Watch(shared.DesiredLRPSchemaRoot)

	go func() {
		defer close(changes)
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

				var before *models.DesiredLRP
				var after *models.DesiredLRP

				if event.Node != nil {
					aft, err := models.NewDesiredLRPFromJSON(event.Node.Value)
					if err != nil {
						continue
					}

					after = &aft
				}

				if event.PrevNode != nil {
					bef, err := models.NewDesiredLRPFromJSON(event.PrevNode.Value)
					if err != nil {
						continue
					}

					before = &bef
				}

				changes <- models.DesiredLRPChange{
					Before: before,
					After:  after,
				}

			case err, ok := <-errsInner:
				if ok {
					errsOuter <- err
				}
				return
			}
		}
	}()

	return changes, stopOuter, errsOuter
}

func watchForActualLRPs(store storeadapter.StoreAdapter) (<-chan models.ActualLRPChange, chan<- bool, <-chan error) {
	changes := make(chan models.ActualLRPChange)
	stopOuter := make(chan bool)
	errsOuter := make(chan error)

	events, stopInner, errsInner := store.Watch(shared.ActualLRPSchemaRoot)

	go func() {
		defer close(changes)
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

				var before *models.ActualLRP
				var after *models.ActualLRP

				if event.Node != nil {
					aft, err := models.NewActualLRPFromJSON(event.Node.Value)
					if err != nil {
						continue
					}

					after = &aft
				}

				if event.PrevNode != nil {
					bef, err := models.NewActualLRPFromJSON(event.PrevNode.Value)
					if err != nil {
						continue
					}

					before = &bef
				}

				changes <- models.ActualLRPChange{
					Before: before,
					After:  after,
				}

			case err, ok := <-errsInner:
				if ok {
					errsOuter <- err
				}
				return
			}
		}
	}()

	return changes, stopOuter, errsOuter
}
