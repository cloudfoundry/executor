package lrp_bbs

import (
	"github.com/cloudfoundry-incubator/runtime-schema/bbs/shared"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/storeadapter"
)

func (bbs *LRPBBS) WatchForDesiredLRPChanges() (<-chan models.DesiredLRPChange, chan<- bool, <-chan error) {
	desired := make(chan models.DesiredLRPChange)

	filter := func(event storeadapter.WatchEvent) (models.DesiredLRPChange, bool) {
		var before *models.DesiredLRP
		var after *models.DesiredLRP

		if event.Node != nil {
			aft, err := models.NewDesiredLRPFromJSON(event.Node.Value)
			if err != nil {
				return models.DesiredLRPChange{}, false
			}

			after = &aft
		}

		if event.PrevNode != nil {
			bef, err := models.NewDesiredLRPFromJSON(event.PrevNode.Value)
			if err != nil {
				return models.DesiredLRPChange{}, false
			}

			before = &bef
		}

		return models.DesiredLRPChange{
			Before: before,
			After:  after,
		}, true

	}

	stop, err := shared.WatchWithFilter(bbs.store, shared.DesiredLRPSchemaRoot, desired, filter)

	return desired, stop, err
}

func (bbs *LRPBBS) WatchForActualLRPChanges() (<-chan models.ActualLRPChange, chan<- bool, <-chan error) {
	actual := make(chan models.ActualLRPChange)

	filter := func(event storeadapter.WatchEvent) (models.ActualLRPChange, bool) {
		var before *models.ActualLRP
		var after *models.ActualLRP

		if event.Node != nil {
			aft, err := models.NewActualLRPFromJSON(event.Node.Value)
			if err != nil {
				return models.ActualLRPChange{}, false
			}

			after = &aft
		}

		if event.PrevNode != nil {
			bef, err := models.NewActualLRPFromJSON(event.PrevNode.Value)
			if err != nil {
				return models.ActualLRPChange{}, false
			}

			before = &bef
		}

		return models.ActualLRPChange{
			Before: before,
			After:  after,
		}, true

	}

	stop, err := shared.WatchWithFilter(bbs.store, shared.ActualLRPSchemaRoot, actual, filter)

	return actual, stop, err
}
