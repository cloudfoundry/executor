package bbs

import (
	"path"
	"time"

	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/storeadapter"
)

const ClaimTTL = 10 * time.Second
const ResolvingTTL = 5 * time.Second
const TaskSchemaRoot = SchemaRoot + "task"
const ExecutorSchemaRoot = SchemaRoot + "executor"
const LockSchemaRoot = SchemaRoot + "locks"

func taskSchemaPath(task models.Task) string {
	return path.Join(TaskSchemaRoot, task.Guid)
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

func watchForTaskModificationsOnState(store storeadapter.StoreAdapter, state models.TaskState) (<-chan models.Task, chan<- bool, <-chan error) {
	tasks := make(chan models.Task)
	stopOuter := make(chan bool)
	errsOuter := make(chan error)

	events, stopInner, errsInner := store.Watch(TaskSchemaRoot)

	go func() {
		defer close(tasks)
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
				switch event.Type {
				case storeadapter.CreateEvent, storeadapter.UpdateEvent:
					task, err := models.NewTaskFromJSON(event.Node.Value)
					if err != nil {
						continue
					}

					if task.State == state {
						tasks <- task
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

	return tasks, stopOuter, errsOuter
}
