package bbs

import (
	"fmt"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/storeadapter"
)

func (self *BBS) GetAllPendingRunOnces() ([]*models.RunOnce, error) {
	all, err := self.GetAllRunOnces()
	return filterRunOnces(all, models.RunOnceStatePending), err
}

func (self *BBS) GetAllClaimedRunOnces() ([]*models.RunOnce, error) {
	all, err := self.GetAllRunOnces()
	return filterRunOnces(all, models.RunOnceStateClaimed), err
}

func (self *BBS) GetAllStartingRunOnces() ([]*models.RunOnce, error) {
	all, err := self.GetAllRunOnces()
	return filterRunOnces(all, models.RunOnceStateRunning), err
}

func (self *BBS) GetAllCompletedRunOnces() ([]*models.RunOnce, error) {
	all, err := self.GetAllRunOnces()
	return filterRunOnces(all, models.RunOnceStateCompleted), err
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

func filterRunOnces(runOnces []*models.RunOnce, state models.RunOnceState) []*models.RunOnce {
	result := make([]*models.RunOnce, 0)
	for _, model := range runOnces {
		if model.State == state {
			result = append(result, model)
		}
	}
	return result
}
