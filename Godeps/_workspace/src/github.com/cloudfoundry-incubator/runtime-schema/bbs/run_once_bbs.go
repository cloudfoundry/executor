package bbs

import (
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/cloudfoundry/storeadapter"
	"path"
	"time"
)

const ClaimTTL = 10 * time.Second
const ResolvingTTL = 5 * time.Second
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
	errsOuter := make(chan error)

	events, stopInner, errsInner := store.Watch(runOnceSchemaPath(state))

	go func() {
		defer close(runOnces)
		defer close(errsOuter)

		for {
			select {
			case <-stopOuter:
				stopInner <- true
				return

			case event, ok := <-events:
				if !ok {
					return
				}
				switch event.Type {
				case storeadapter.CreateEvent, storeadapter.UpdateEvent:
					runOnce, err := models.NewRunOnceFromJSON(event.Node.Value)
					if err != nil {
						continue
					}
					runOnces <- runOnce
				}

			case err, ok := <-errsInner:
				if ok {
					errsOuter <- err
				}
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
		runOnce, err := models.NewRunOnceFromJSON(node.Value)
		if err != nil {
			steno.NewLogger("bbs").Errorf("cannot parse runOnce JSON for key %s: %s", node.Key, err.Error())
		} else {
			runOnces = append(runOnces, runOnce)
		}
	}

	return runOnces, nil
}

func verifyExecutorIsPresent(node storeadapter.StoreNode, executorState storeadapter.StoreNode) bool {
	runOnce, err := models.NewRunOnceFromJSON(node.Value)
	if err != nil {
		steno.NewLogger("bbs").Errorf("cannot parse runOnce JSON for key %s: %s", node.Key, err.Error())
		return false
	}
	_, executorIsAlive := executorState.Lookup(runOnce.ExecutorID)
	return executorIsAlive
}

func failedRunOnceNodeFromNode(node storeadapter.StoreNode, failureMessage string) storeadapter.StoreNode {
	runOnce, err := models.NewRunOnceFromJSON(node.Value)
	if err != nil {
		steno.NewLogger("bbs").Errorf("cannot parse runOnce JSON for key %s: %s", node.Key, err.Error())
		runOnce.Guid = path.Base(node.Key)
	}

	runOnce.Failed = true
	runOnce.FailureReason = failureMessage
	return storeadapter.StoreNode{
		Key:   runOnceSchemaPath("completed", runOnce.Guid),
		Value: runOnce.ToJSON(),
	}
}
