package task_bbs

import (
	"github.com/cloudfoundry-incubator/runtime-schema/bbs/shared"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/storeadapter"
)

func (bbs *TaskBBS) GetAllTasks() ([]models.Task, error) {
	node, err := bbs.store.ListRecursively(shared.TaskSchemaRoot)
	if err == storeadapter.ErrorKeyNotFound {
		return []models.Task{}, nil
	}

	if err != nil {
		return []models.Task{}, err
	}

	tasks := []models.Task{}
	for _, node := range node.ChildNodes {
		task, err := models.NewTaskFromJSON(node.Value)
		if err != nil {
			bbs.logger.Errorf("cannot parse task JSON for key %s: %s", node.Key, err.Error())
		} else {
			tasks = append(tasks, task)
		}
	}

	return tasks, nil
}

func (self *TaskBBS) GetAllPendingTasks() ([]models.Task, error) {
	all, err := self.GetAllTasks()
	return filterTasks(all, models.TaskStatePending), err
}

func (self *TaskBBS) GetAllClaimedTasks() ([]models.Task, error) {
	all, err := self.GetAllTasks()
	return filterTasks(all, models.TaskStateClaimed), err
}

func (self *TaskBBS) GetAllStartingTasks() ([]models.Task, error) {
	all, err := self.GetAllTasks()
	return filterTasks(all, models.TaskStateRunning), err
}

func (self *TaskBBS) GetAllCompletedTasks() ([]models.Task, error) {
	all, err := self.GetAllTasks()
	return filterTasks(all, models.TaskStateCompleted), err
}

func filterTasks(tasks []models.Task, state models.TaskState) []models.Task {
	result := make([]models.Task, 0)
	for _, model := range tasks {
		if model.State == state {
			result = append(result, model)
		}
	}
	return result
}
