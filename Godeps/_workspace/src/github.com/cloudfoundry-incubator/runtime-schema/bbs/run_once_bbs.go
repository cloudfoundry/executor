package bbs

import (
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/storeadapter"
	"path"
	"time"
)

const ClaimTTL uint64 = 10 //seconds
const RunOnceSchemaRoot = SchemaRoot + "run_once"
const ExecutorSchemaRoot = SchemaRoot + "executor"

func runOnceSchemaPath(segments ...string) string {
	return path.Join(append([]string{RunOnceSchemaRoot}, segments...)...)
}

func executorSchemaPath(segments ...string) string {
	return path.Join(append([]string{ExecutorSchemaRoot}, segments...)...)
}

func retryIndefinitelyOnStoreTimeout(callback func() error) error {
	for {
		err := callback()

		if err == storeadapter.ErrorTimeout {
			time.Sleep(time.Second)
			continue
		}

		return err
	}
}

func watchForRunOnceModificationsOnState(store storeadapter.StoreAdapter, state string) (<-chan models.RunOnce, chan<- bool, <-chan error) {
	runOnces := make(chan models.RunOnce)
	stopOuter := make(chan bool)
	errsOuter := make(chan error, 1)

	events, stopInner, errsInner := store.Watch(runOnceSchemaPath(state))

	go func() {
		for {
			select {
			case <-stopOuter:
				stopInner <- true
				close(runOnces)
				return

			case event := <-events:
				switch event.Type {
				case storeadapter.CreateEvent, storeadapter.UpdateEvent:
					runOnce, err := models.NewRunOnceFromJSON(event.Node.Value)
					if err != nil {
						continue
					}

					runOnces <- runOnce
				}

			case err := <-errsInner:
				errsOuter <- err
				return
			}
		}
	}()

	return runOnces, stopOuter, errsOuter
}

func getAllRunOnces(store storeadapter.StoreAdapter, state string) ([]models.RunOnce, error) {
	node, err := store.ListRecursively(runOnceSchemaPath(state))
	if err == storeadapter.ErrorKeyNotFound {
		return []models.RunOnce{}, nil
	}

	if err != nil {
		return []models.RunOnce{}, err
	}

	runOnces := []models.RunOnce{}
	for _, node := range node.ChildNodes {
		runOnce, _ := models.NewRunOnceFromJSON(node.Value)
		runOnces = append(runOnces, runOnce)
	}

	return runOnces, nil
}

func verifyExecutorIsPresent(node storeadapter.StoreNode, executorState storeadapter.StoreNode) bool {
	runOnce, _ := models.NewRunOnceFromJSON(node.Value)
	_, executorIsAlive := executorState.Lookup(runOnce.ExecutorID)
	return executorIsAlive
}

func failedRunOnceNodeFromNode(node storeadapter.StoreNode, failureMessage string) storeadapter.StoreNode {
	runOnce, _ := models.NewRunOnceFromJSON(node.Value)
	runOnce.Failed = true
	runOnce.FailureReason = failureMessage
	return storeadapter.StoreNode{
		Key:   runOnceSchemaPath("completed", runOnce.Guid),
		Value: runOnce.ToJSON(),
	}
}
