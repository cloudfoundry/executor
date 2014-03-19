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
const LockSchemaRoot = SchemaRoot + "locks"

func runOnceSchemaPath(runOnce *models.RunOnce) string {
	return path.Join(RunOnceSchemaRoot, runOnce.Guid)
}

func executorSchemaPath(executorID string) string {
	return path.Join(ExecutorSchemaRoot, executorID)
}

func lockSchemaPath(lockName string) string {
	return path.Join(LockSchemaRoot, lockName)
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

func watchForRunOnceModificationsOnState(store storeadapter.StoreAdapter, state models.RunOnceState) (<-chan *models.RunOnce, chan<- bool, <-chan error) {
	runOnces := make(chan *models.RunOnce)
	stopOuter := make(chan bool)
	errsOuter := make(chan error)

	events, stopInner, errsInner := store.Watch(RunOnceSchemaRoot)

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

					if runOnce.State == state {
						runOnces <- &runOnce
					}
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

func getAllRunOnces(store storeadapter.StoreAdapter, state models.RunOnceState) ([]*models.RunOnce, error) {
	node, err := store.ListRecursively(RunOnceSchemaRoot)
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
		} else if runOnce.State == state {
			runOnces = append(runOnces, &runOnce)
		}
	}

	return runOnces, nil
}
