package bbs

import (
	"fmt"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/storeadapter"
)

func (self *BBS) GetAllPendingTasks() ([]*models.Task, error) {
	all, err := self.GetAllTasks()
	return filterTasks(all, models.TaskStatePending), err
}

func (self *BBS) GetAllClaimedTasks() ([]*models.Task, error) {
	all, err := self.GetAllTasks()
	return filterTasks(all, models.TaskStateClaimed), err
}

func (self *BBS) GetAllStartingTasks() ([]*models.Task, error) {
	all, err := self.GetAllTasks()
	return filterTasks(all, models.TaskStateRunning), err
}

func (self *BBS) GetAllCompletedTasks() ([]*models.Task, error) {
	all, err := self.GetAllTasks()
	return filterTasks(all, models.TaskStateCompleted), err
}

func (self *BBS) GetAllExecutors() ([]string, error) {
	nodes, err := self.store.ListRecursively(ExecutorSchemaRoot)
	if err == storeadapter.ErrorKeyNotFound {
		return []string{}, nil
	} else if err != nil {
		return nil, err
	}

	executors := []string{}

	for _, node := range nodes.ChildNodes {
		executors = append(executors, node.KeyComponents()[2])
	}

	return executors, nil
}

func (self *BBS) printNodes(message string, nodes []storeadapter.StoreNode) {
	fmt.Println(message)
	for _, node := range nodes {
		fmt.Printf("    %s [%d]: %s\n", node.Key, node.TTL, string(node.Value))
	}
}

func filterTasks(tasks []*models.Task, state models.TaskState) []*models.Task {
	result := make([]*models.Task, 0)
	for _, model := range tasks {
		if model.State == state {
			result = append(result, model)
		}
	}
	return result
}
