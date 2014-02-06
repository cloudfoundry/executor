package bbs

import (
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/storeadapter"
	"path"
	"time"
)

const ClaimTTL uint64 = 10 //seconds
const SchemaRoot = "/v1/"
const RunOnceSchemaRoot = SchemaRoot + "run_once"
const ExecutorSchemaRoot = SchemaRoot + "executor"

type executorBBS struct {
	store storeadapter.StoreAdapter
}

type stagerBBS struct {
	store storeadapter.StoreAdapter
}

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
	errsOuter := make(chan error, 1)

	events, stopInner, errsInner := store.Watch(runOnceSchemaPath(state))

	go func() {
		for {
			select {
			case <-stopOuter:
				stopInner <- true
				close(runOnces)
				return

			case event := <-events:
				switch event.Type {
				case storeadapter.CreateEvent, storeadapter.UpdateEvent:
					runOnce, err := models.NewRunOnceFromJSON(event.Node.Value)
					if err != nil {
						continue
					}

					runOnces <- runOnce
				}

			case err := <-errsInner:
				errsOuter <- err
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
		runOnce, _ := models.NewRunOnceFromJSON(node.Value)
		runOnces = append(runOnces, runOnce)
	}

	return runOnces, nil
}

func (self *BBS) GetAllPendingRunOnces() ([]models.RunOnce, error) {
	return getAllRunOnces(self.store, "pending")
}

func (self *BBS) GetAllClaimedRunOnces() ([]models.RunOnce, error) {
	return getAllRunOnces(self.store, "claimed")
}

func (self *BBS) GetAllStartingRunOnces() ([]models.RunOnce, error) {
	return getAllRunOnces(self.store, "running")
}

func (self *BBS) GetAllCompletedRunOnces() ([]models.RunOnce, error) {
	return getAllRunOnces(self.store, "completed")
}

func (self *stagerBBS) WatchForCompletedRunOnce() (<-chan models.RunOnce, chan<- bool, <-chan error) {
	return watchForRunOnceModificationsOnState(self.store, "completed")
}

// The stager calls this when it wants to desire a payload
// stagerBBS will retry this repeatedly if it gets a StoreTimeout error (up to N seconds?)
// If this fails, the stager should bail and run its "this-failed-to-stage" routine
func (self *stagerBBS) DesireRunOnce(runOnce models.RunOnce) error {
	return retryIndefinitelyOnStoreTimeout(func() error {
		return self.store.SetMulti([]storeadapter.StoreNode{
			{
				Key:   runOnceSchemaPath("pending", runOnce.Guid),
				Value: runOnce.ToJSON(),
			},
		})
	})
}

// The stager calls this when it wants to signal that it has received a completion and is handling it
// stagerBBS will retry this repeatedly if it gets a StoreTimeout error (up to N seconds?)
// If this fails, the stager should assume that someone else is handling the completion and should bail
func (self *stagerBBS) ResolveRunOnce(runOnce models.RunOnce) error {
	return retryIndefinitelyOnStoreTimeout(func() error {
		return self.store.Delete(runOnceSchemaPath("pending", runOnce.Guid))
	})
}

func (self *executorBBS) MaintainExecutorPresence(heartbeatIntervalInSeconds uint64, executorId string) (chan bool, chan error, error) {
	err := self.store.SetMulti([]storeadapter.StoreNode{
		{
			Key:   executorSchemaPath(executorId),
			Value: []byte{},
			TTL:   heartbeatIntervalInSeconds,
		},
	})

	if err != nil {
		return nil, nil, err
	}

	stop := make(chan bool)
	errors := make(chan error)

	go func() {
		ticker := time.NewTicker(time.Duration(heartbeatIntervalInSeconds) * time.Second / 2)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				err := self.store.Update(storeadapter.StoreNode{
					Key:   executorSchemaPath(executorId),
					Value: []byte{},
					TTL:   heartbeatIntervalInSeconds,
				})

				if err != nil {
					errors <- err
					return
				}
			case <-stop:
				return
			}
		}
	}()

	return stop, errors, nil
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

		storeNodesToSet = append(storeNodesToSet, pendingNode)
	}

	for _, node := range []storeadapter.StoreNode{claimed, running, completed} {
		for _, node := range node.ChildNodes {
			guid := node.KeyComponents()[2]

			_, isPending := pending.Lookup(guid)
			if !isPending {
				keysToDelete = append(keysToDelete, node.Key)
			}
		}
	}

	self.store.SetMulti(storeNodesToSet)
	self.store.Delete(keysToDelete...)
}

func verifyExecutorIsPresent(node storeadapter.StoreNode, executorState storeadapter.StoreNode) bool {
	runOnce, _ := models.NewRunOnceFromJSON(node.Value)
	_, executorIsAlive := executorState.Lookup(runOnce.ExecutorID)
	return executorIsAlive
}

func failedRunOnceNodeFromNode(node storeadapter.StoreNode, failureMessage string) storeadapter.StoreNode {
	runOnce, _ := models.NewRunOnceFromJSON(node.Value)
	runOnce.Failed = true
	runOnce.FailureReason = failureMessage
	return storeadapter.StoreNode{
		Key:   runOnceSchemaPath("completed", runOnce.Guid),
		Value: runOnce.ToJSON(),
	}
}

func (self *executorBBS) GrabRunOnceLock(duration time.Duration) (bool, error) {
	err := self.store.Create(storeadapter.StoreNode{
		Key:   runOnceSchemaPath("lock"),
		Value: []byte("placeholder data"),
		TTL:   uint64(duration.Seconds()),
	})

	return (err == nil), err
}
