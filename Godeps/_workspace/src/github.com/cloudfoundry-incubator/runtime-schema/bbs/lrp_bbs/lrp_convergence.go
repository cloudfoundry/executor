package lrp_bbs

import (
	"sync"

	"github.com/cloudfoundry-incubator/delta_force"
	"github.com/cloudfoundry-incubator/runtime-schema/bbs/shared"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/storeadapter"
)

type compareAndSwappableDesiredLRP struct {
	OldIndex      uint64
	NewDesiredLRP models.DesiredLRP
}

func (bbs *LRPBBS) ConvergeLRPs() {
	actualsByProcessGuid, err := bbs.pruneActualsWithMissingExecutors()
	if err != nil {
		bbs.logger.Errord(map[string]interface{}{
			"error": err.Error(),
		}, "lrp-converger.failed-to-fetch-and-prune-actual-lrps")
		return
	}

	node, err := bbs.store.ListRecursively(shared.DesiredLRPSchemaRoot)
	if err != nil && err != storeadapter.ErrorKeyNotFound {
		bbs.logger.Errord(map[string]interface{}{
			"error": err.Error(),
		}, "lrp-converger.failed-to-fetch-desired-lrps")
		return
	}

	var desiredLRPsToCAS []compareAndSwappableDesiredLRP
	var keysToDelete []string
	knownDesiredProcessGuids := map[string]bool{}

	for _, node := range node.ChildNodes {
		desiredLRP, err := models.NewDesiredLRPFromJSON(node.Value)

		if err != nil {
			bbs.logger.Infod(map[string]interface{}{
				"error": err.Error(),
			}, "lrp-converger.pruning-unparseable-desired-lrp-json")
			keysToDelete = append(keysToDelete, node.Key)
			continue
		}

		knownDesiredProcessGuids[desiredLRP.ProcessGuid] = true
		actualLRPsForDesired := actualsByProcessGuid[desiredLRP.ProcessGuid]

		if bbs.needsReconciliation(desiredLRP, actualLRPsForDesired) {
			desiredLRPsToCAS = append(desiredLRPsToCAS, compareAndSwappableDesiredLRP{
				OldIndex:      node.Index,
				NewDesiredLRP: desiredLRP,
			})
		}
	}

	stopLRPInstances := bbs.instancesToStop(knownDesiredProcessGuids, actualsByProcessGuid)

	bbs.store.Delete(keysToDelete...)
	bbs.batchCompareAndSwapDesiredLRPs(desiredLRPsToCAS)
	err = bbs.RequestStopLRPInstances(stopLRPInstances)
	if err != nil {
		bbs.logger.Errord(map[string]interface{}{
			"error": err.Error(),
		}, "lrp-converger.failed-to-request-stops")
	}
}

func (bbs *LRPBBS) instancesToStop(knownDesiredProcessGuids map[string]bool, actualsByProcessGuid map[string][]models.ActualLRP) []models.StopLRPInstance {
	var stopLRPInstances []models.StopLRPInstance

	for processGuid, actuals := range actualsByProcessGuid {
		if !knownDesiredProcessGuids[processGuid] {
			for _, actual := range actuals {
				bbs.logger.Infod(map[string]interface{}{
					"process-guid":  processGuid,
					"instance-guid": actual.InstanceGuid,
					"index":         actual.Index,
				}, "lrp-converger.detected-undesired-process")

				stopLRPInstances = append(stopLRPInstances, models.StopLRPInstance{
					ProcessGuid:  processGuid,
					InstanceGuid: actual.InstanceGuid,
					Index:        actual.Index,
				})
			}
		}
	}

	return stopLRPInstances
}

func (bbs *LRPBBS) needsReconciliation(desiredLRP models.DesiredLRP, actualLRPsForDesired []models.ActualLRP) bool {
	var actuals delta_force.ActualInstances
	for _, actualLRP := range actualLRPsForDesired {
		actuals = append(actuals, delta_force.ActualInstance{
			Index: actualLRP.Index,
			Guid:  actualLRP.InstanceGuid,
		})
	}
	result := delta_force.Reconcile(desiredLRP.Instances, actuals)

	if len(result.IndicesToStart) > 0 {
		bbs.logger.Infod(map[string]interface{}{
			"process-guid":      desiredLRP.ProcessGuid,
			"desired-instances": desiredLRP.Instances,
			"missing-indices":   result.IndicesToStart,
		}, "lrp-converger.detected-missing-instance")
	}
	if len(result.GuidsToStop) > 0 {
		bbs.logger.Infod(map[string]interface{}{
			"process-guid":      desiredLRP.ProcessGuid,
			"desired-instances": desiredLRP.Instances,
			"extra-guids":       result.GuidsToStop,
		}, "lrp-converger.detected-extra-instance")
	}
	if len(result.IndicesToStopOneGuid) > 0 {
		bbs.logger.Infod(map[string]interface{}{
			"process-guid":       desiredLRP.ProcessGuid,
			"desired-instances":  desiredLRP.Instances,
			"duplicated-indices": result.IndicesToStopOneGuid,
		}, "lrp-converger.detected-duplicate-instance")
	}

	return !result.Empty()
}

func (bbs *LRPBBS) pruneActualsWithMissingExecutors() (map[string][]models.ActualLRP, error) {
	executorState, err := bbs.store.ListRecursively(shared.ExecutorSchemaRoot)
	if err == storeadapter.ErrorKeyNotFound {
		executorState = storeadapter.StoreNode{}
	} else if err != nil {
		bbs.logger.Errord(map[string]interface{}{
			"error": err.Error(),
		}, "lrp-converger.get-executors.failed")
		return nil, err
	}

	actuals, err := bbs.GetAllActualLRPs()
	if err != nil {
		bbs.logger.Errord(map[string]interface{}{
			"error": err.Error(),
		}, "lrp-converger.get-actuals.failed")
		return nil, err
	}

	keysToDelete := []string{}
	actualsByProcessGuid := map[string][]models.ActualLRP{}

	for _, actual := range actuals {
		_, executorIsAlive := executorState.Lookup(actual.ExecutorID)

		if executorIsAlive {
			actualsByProcessGuid[actual.ProcessGuid] = append(actualsByProcessGuid[actual.ProcessGuid], actual)
		} else {
			bbs.logger.Infod(map[string]interface{}{
				"actual":      actual,
				"executor-id": actual.ExecutorID,
			}, "lrp-converger.identified-actual-with-missing-executor")

			keysToDelete = append(keysToDelete, shared.ActualLRPSchemaPath(actual))
		}
	}

	bbs.store.Delete(keysToDelete...)
	return actualsByProcessGuid, nil
}

func (bbs *LRPBBS) batchCompareAndSwapDesiredLRPs(desiredLRPsToCAS []compareAndSwappableDesiredLRP) {
	waitGroup := &sync.WaitGroup{}
	waitGroup.Add(len(desiredLRPsToCAS))
	for _, desiredLRPToCAS := range desiredLRPsToCAS {
		desiredLRP := desiredLRPToCAS.NewDesiredLRP
		newStoreNode := storeadapter.StoreNode{
			Key:   shared.DesiredLRPSchemaPath(desiredLRP),
			Value: desiredLRP.ToJSON(),
		}

		go func(desiredLRPToCAS compareAndSwappableDesiredLRP, newStoreNode storeadapter.StoreNode) {
			err := bbs.store.CompareAndSwapByIndex(desiredLRPToCAS.OldIndex, newStoreNode)
			if err != nil {
				bbs.logger.Errord(map[string]interface{}{
					"error": err.Error(),
				}, "lrp_bbs.converge.failed-to-compare-and-swap")
			}
			waitGroup.Done()
		}(desiredLRPToCAS, newStoreNode)
	}

	waitGroup.Wait()
}
