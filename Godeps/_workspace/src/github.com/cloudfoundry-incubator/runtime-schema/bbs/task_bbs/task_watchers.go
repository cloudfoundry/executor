package task_bbs

import (
	"github.com/cloudfoundry-incubator/runtime-schema/bbs/shared"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/storeadapter"
)

func (bbs *TaskBBS) WatchForDesiredTask() (<-chan models.Task, chan<- bool, <-chan error) {
	tasks := make(chan models.Task)
	stop, err := shared.WatchWithFilter(bbs.store, shared.TaskSchemaRoot, tasks, taskFilterByState(models.TaskStatePending))
	return tasks, stop, err
}

func (bbs *TaskBBS) WatchForCompletedTask() (<-chan models.Task, chan<- bool, <-chan error) {
	tasks := make(chan models.Task)
	stop, err := shared.WatchWithFilter(bbs.store, shared.TaskSchemaRoot, tasks, taskFilterByState(models.TaskStateCompleted))
	return tasks, stop, err
}

func taskFilterByState(state models.TaskState) func(storeadapter.WatchEvent) (models.Task, bool) {
	return func(event storeadapter.WatchEvent) (models.Task, bool) {

		switch event.Type {
		case storeadapter.CreateEvent, storeadapter.UpdateEvent:
			task, err := models.NewTaskFromJSON(event.Node.Value)
			if err != nil {
				return models.Task{}, false
			}

			if task.State == state {
				return task, true
			}
		}

		return models.Task{}, false
	}
}
