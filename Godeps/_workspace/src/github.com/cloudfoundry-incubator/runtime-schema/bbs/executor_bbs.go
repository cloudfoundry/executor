package bbs

import (
	"time"

	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/storeadapter"
)

type executorBBS struct {
	store       storeadapter.StoreAdapter
	timeToClaim time.Duration
}

// This is the time limit for the executor pool to claim a pending timeout.
func (self *executorBBS) SetTimeToClaim(timeToClaim time.Duration) {
	self.timeToClaim = timeToClaim
}

func (self *executorBBS) MaintainExecutorPresence(heartbeatIntervalInSeconds uint64, executorId string) (PresenceInterface, chan error, error) {
	presence := NewPresence(self.store, executorSchemaPath(executorId), []byte{})
	errors, err := presence.Maintain(heartbeatIntervalInSeconds)
	return presence, errors, err
}

func (self *executorBBS) WatchForDesiredRunOnce() (<-chan models.RunOnce, chan<- bool, <-chan error) {
	return watchForRunOnceModificationsOnState(self.store, "pending")
}

// The executor calls this when it wants to claim a runonce
// stagerBBS will retry this repeatedly if it gets a StoreTimeout error (up to N seconds?)
// If this fails, the executor should assume that someone else is handling the claim and should bail
func (self *executorBBS) ClaimRunOnce(runOnce models.RunOnce) error {
	if runOnce.ExecutorID == "" {
		panic("must set ExecutorID on RunOnce model to claim (finish your tests)")
	}

	return retryIndefinitelyOnStoreTimeout(func() error {
		return self.store.Create(storeadapter.StoreNode{
			Key:   runOnceSchemaPath("claimed", runOnce.Guid),
			Value: runOnce.ToJSON(),
			TTL:   ClaimTTL,
		})
	})
}

// The executor calls this when it is about to run the runonce in the claimed container
// stagerBBS will retry this repeatedly if it gets a StoreTimeout error (up to N seconds?)
// If this fails, the executor should assume that someone else is running and should clean up and bail
func (self *executorBBS) StartRunOnce(runOnce models.RunOnce) error {
	if runOnce.ExecutorID == "" {
		panic("must set ExecutorID on RunOnce model to start (finish your tests)")
	}

	if runOnce.ContainerHandle == "" {
		panic("must set ContainerHandle on RunOnce model to start (finish your tests)")
	}

	return retryIndefinitelyOnStoreTimeout(func() error {
		return self.store.Create(storeadapter.StoreNode{
			Key:   runOnceSchemaPath("running", runOnce.Guid),
			Value: runOnce.ToJSON(),
		})
	})
}

// The executor calls this when it has finished running the runonce (be it success or failure)
// stagerBBS will retry this repeatedly if it gets a StoreTimeout error (up to N seconds?)
// This really really shouldn't fail.  If it does, blog about it and walk away. If it failed in a
// consistent way (i.e. key already exists), there's probably a flaw in our design.
func (self *executorBBS) CompleteRunOnce(runOnce models.RunOnce) error {
	return retryIndefinitelyOnStoreTimeout(func() error {
		return self.store.Create(storeadapter.StoreNode{
			Key:   runOnceSchemaPath("completed", runOnce.Guid),
			Value: runOnce.ToJSON(),
		})
	})
}

// ConvergeRunOnce is run by *one* executor every X seconds (doesn't really matter what X is.. pick something performant)
// Converge will:
// 1. Kick (by setting) any pending for guids that only have a pending
// 2. Kick (by setting) any completed for guids that have a pending
// 3. Remove any claimed/running/completed for guids that have no corresponding pending
func (self *executorBBS) ConvergeRunOnce() {
	runOnceState, err := self.store.ListRecursively(RunOnceSchemaRoot)
	if err != nil {
		return
	}

	executorState, err := self.store.ListRecursively(ExecutorSchemaRoot)
	if err == storeadapter.ErrorKeyNotFound {
		executorState = storeadapter.StoreNode{}
	} else if err != nil {
		return
	}

	storeNodesToSet := []storeadapter.StoreNode{}
	keysToDelete := []string{}

	pending, _ := runOnceState.Lookup("pending")
	claimed, _ := runOnceState.Lookup("claimed")
	running, _ := runOnceState.Lookup("running")
	completed, _ := runOnceState.Lookup("completed")

	unclaimedTimeoutBoundary := time.Now().Add(-self.timeToClaim).UnixNano()

	for _, pendingNode := range pending.ChildNodes {
		guid := pendingNode.KeyComponents()[3]

		completedNode, isCompleted := completed.Lookup(guid)
		if isCompleted {
			storeNodesToSet = append(storeNodesToSet, completedNode)
			continue
		}

		claimedNode, isClaimed := claimed.Lookup(guid)

		if isClaimed {
			if !verifyExecutorIsPresent(claimedNode, executorState) {
				storeNodesToSet = append(storeNodesToSet, failedRunOnceNodeFromNode(claimedNode, "executor disappeared before completion"))
			}
			continue
		}

		runningNode, isRunning := running.Lookup(guid)

		if isRunning {
			if !verifyExecutorIsPresent(runningNode, executorState) {
				storeNodesToSet = append(storeNodesToSet, failedRunOnceNodeFromNode(runningNode, "executor disappeared before completion"))
			}
			continue
		}

		runOnce, err := models.NewRunOnceFromJSON(pendingNode.Value)
		if err != nil {
			pendingNode.Value = nil
			storeNodesToSet = append(storeNodesToSet, failedRunOnceNodeFromNode(pendingNode, "corrupt pending node"))
			continue
		}

		if runOnce.CreatedAt <= unclaimedTimeoutBoundary {
			storeNodesToSet = append(storeNodesToSet, failedRunOnceNodeFromNode(pendingNode, "not claimed within time limit"))
			continue
		}

		storeNodesToSet = append(storeNodesToSet, pendingNode)

	}

	for _, node := range []storeadapter.StoreNode{claimed, running, completed} {
		for _, node := range node.ChildNodes {
			guid := node.KeyComponents()[3]

			_, isPending := pending.Lookup(guid)
			if !isPending {
				keysToDelete = append(keysToDelete, node.Key)
			}
		}
	}

	self.store.SetMulti(storeNodesToSet)
	self.store.Delete(keysToDelete...)
}

func (self *executorBBS) GrabRunOnceLock(duration time.Duration) (bool, error) {
	err := self.store.Create(storeadapter.StoreNode{
		Key:   runOnceSchemaPath("lock"),
		Value: []byte("placeholder data"),
		TTL:   uint64(duration.Seconds()),
	})

	return (err == nil), err
}
