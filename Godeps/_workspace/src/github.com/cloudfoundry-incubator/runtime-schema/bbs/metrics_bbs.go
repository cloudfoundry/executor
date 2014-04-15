package bbs

import (
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/cloudfoundry/storeadapter"
)

type metricsBBS struct {
	store storeadapter.StoreAdapter
}

func (bbs *metricsBBS) GetAllRunOnces() ([]*models.RunOnce, error) {
	node, err := bbs.store.ListRecursively(RunOnceSchemaRoot)
	if err == storeadapter.ErrorKeyNotFound {
		return []*models.RunOnce{}, nil
	}

	if err != nil {
		return []*models.RunOnce{}, err
	}

	runOnces := []*models.RunOnce{}
	for _, node := range node.ChildNodes {
		runOnce, err := models.NewRunOnceFromJSON(node.Value)
		if err != nil {
			steno.NewLogger("bbs").Errorf("cannot parse runOnce JSON for key %s: %s", node.Key, err.Error())
		} else {
			runOnces = append(runOnces, &runOnce)
		}
	}

	return runOnces, nil
}
