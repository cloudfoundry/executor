package bbs

import (
	"fmt"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/storeadapter"
)

func (self *BBS) GetAllPendingRunOnces() ([]*models.RunOnce, error) {
	return getAllRunOnces(self.store, models.RunOnceStatePending)
}

func (self *BBS) GetAllClaimedRunOnces() ([]*models.RunOnce, error) {
	return getAllRunOnces(self.store, models.RunOnceStateClaimed)
}

func (self *BBS) GetAllStartingRunOnces() ([]*models.RunOnce, error) {
	return getAllRunOnces(self.store, models.RunOnceStateRunning)
}

func (self *BBS) GetAllCompletedRunOnces() ([]*models.RunOnce, error) {
	return getAllRunOnces(self.store, models.RunOnceStateCompleted)
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
