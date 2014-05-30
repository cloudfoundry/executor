package lrp_bbs

import (
	"fmt"

	"github.com/cloudfoundry-incubator/runtime-schema/bbs/shared"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/storeadapter"
)

func (bbs *LRPBBS) RequestStopLRPInstance(stopInstance models.StopLRPInstance) error {
	return shared.RetryIndefinitelyOnStoreTimeout(func() error {
		return bbs.store.SetMulti([]storeadapter.StoreNode{
			{
				Key:   shared.StopLRPInstanceSchemaPath(stopInstance),
				Value: stopInstance.ToJSON(),
			},
		})
	})
}

func (bbs *LRPBBS) GetAllStopLRPInstances() ([]models.StopLRPInstance, error) {
	stopInstances := []models.StopLRPInstance{}

	node, err := bbs.store.ListRecursively(shared.StopLRPInstanceSchemaRoot)
	if err == storeadapter.ErrorKeyNotFound {
		return stopInstances, nil
	}

	if err != nil {
		return stopInstances, err
	}

	for _, node := range node.ChildNodes {
		lrp, err := models.NewStopLRPInstanceFromJSON(node.Value)
		if err != nil {
			return stopInstances, fmt.Errorf("cannot parse lrp JSON for key %s: %s", node.Key, err.Error())
		} else {
			stopInstances = append(stopInstances, lrp)
		}
	}

	return stopInstances, nil
}

func (bbs *LRPBBS) ResolveStopLRPInstance(stopInstance models.StopLRPInstance) error {
	return shared.RetryIndefinitelyOnStoreTimeout(func() error {
		err := bbs.store.Delete(shared.StopLRPInstanceSchemaPath(stopInstance), shared.ActualLRPSchemaPathFromStopLRPInstance(stopInstance))
		if err == storeadapter.ErrorKeyNotFound {
			err = nil
		}
		return err
	})
}

func (bbs *LRPBBS) WatchForStopLRPInstance() (<-chan models.StopLRPInstance, chan<- bool, <-chan error) {
	stopInstances := make(chan models.StopLRPInstance)

	filter := func(event storeadapter.WatchEvent) (models.StopLRPInstance, bool) {
		switch event.Type {
		case storeadapter.CreateEvent, storeadapter.UpdateEvent:
			stopInstance, err := models.NewStopLRPInstanceFromJSON(event.Node.Value)
			if err != nil {
				return models.StopLRPInstance{}, false
			}
			return stopInstance, true
		}
		return models.StopLRPInstance{}, false
	}

	stop, errs := shared.WatchWithFilter(bbs.store, shared.StopLRPInstanceSchemaRoot, stopInstances, filter)

	return stopInstances, stop, errs
}
