package task_bbs

import (
	"github.com/cloudfoundry-incubator/runtime-schema/bbs/shared"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/storeadapter"
)

func (self *TaskBBS) WatchForDesiredTask() (<-chan models.Task, chan<- bool, <-chan error) {
	return watchForTaskModificationsOnState(self.store, models.TaskStatePending)
}

func (s *TaskBBS) WatchForCompletedTask() (<-chan models.Task, chan<- bool, <-chan error) {
	return watchForTaskModificationsOnState(s.store, models.TaskStateCompleted)
}

func watchForTaskModificationsOnState(store storeadapter.StoreAdapter, state models.TaskState) (<-chan models.Task, chan<- bool, <-chan error) {
	tasks := make(chan models.Task)
	stopOuter := make(chan bool)
	errsOuter := make(chan error)

	events, stopInner, errsInner := store.Watch(shared.TaskSchemaRoot)

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
