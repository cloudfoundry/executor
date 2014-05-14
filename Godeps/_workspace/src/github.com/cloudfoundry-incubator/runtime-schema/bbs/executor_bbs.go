package bbs

import (
	"time"

	"github.com/cloudfoundry/gosteno"
	"github.com/cloudfoundry/gunk/timeprovider"

	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/storeadapter"
)

type executorBBS struct {
	store        storeadapter.StoreAdapter
	timeProvider timeprovider.TimeProvider
}

func (self *executorBBS) MaintainExecutorPresence(heartbeatInterval time.Duration, executorId string) (Presence, <-chan bool, error) {
	presence := NewPresence(self.store, executorSchemaPath(executorId), []byte{})
	status, err := presence.Maintain(heartbeatInterval)
	return presence, status, err
}

func (self *executorBBS) WatchForDesiredTask() (<-chan models.Task, chan<- bool, <-chan error) {
	return watchForTaskModificationsOnState(self.store, models.TaskStatePending)
}

func (self *executorBBS) WatchForDesiredTransitionalLongRunningProcess() (<-chan models.TransitionalLongRunningProcess, chan<- bool, <-chan error) {
	return watchForLrpModificationsOnState(self.store, models.TransitionalLRPStateDesired)
}

// The executor calls this when it wants to claim a task
// stagerBBS will retry this repeatedly if it gets a StoreTimeout error (up to N seconds?)
// If this fails, the executor should assume that someone else is handling the claim and should bail
func (self *executorBBS) ClaimTask(task models.Task, executorID string) (models.Task, error) {
	originalValue := task.ToJSON()

	task.UpdatedAt = self.timeProvider.Time().UnixNano()

	task.State = models.TaskStateClaimed
	task.ExecutorID = executorID

	err := retryIndefinitelyOnStoreTimeout(func() error {
		return self.store.CompareAndSwap(storeadapter.StoreNode{
			Key:   taskSchemaPath(task),
			Value: originalValue,
		}, storeadapter.StoreNode{
			Key:   taskSchemaPath(task),
			Value: task.ToJSON(),
		})
	})

	return task, err
}

// The executor calls this when it is about to run the task in the claimed container
// stagerBBS will retry this repeatedly if it gets a StoreTimeout error (up to N seconds?)
// If this fails, the executor should assume that someone else is running and should clean up and bail
func (self *executorBBS) StartTask(task models.Task, containerHandle string) (models.Task, error) {
	originalValue := task.ToJSON()

	task.UpdatedAt = self.timeProvider.Time().UnixNano()

	task.State = models.TaskStateRunning
	task.ContainerHandle = containerHandle

	err := retryIndefinitelyOnStoreTimeout(func() error {
		return self.store.CompareAndSwap(storeadapter.StoreNode{
			Key:   taskSchemaPath(task),
			Value: originalValue,
		}, storeadapter.StoreNode{
			Key:   taskSchemaPath(task),
			Value: task.ToJSON(),
		})
	})

	return task, err
}

func (self *executorBBS) StartTransitionalLongRunningProcess(lrp models.TransitionalLongRunningProcess) error {
	originalValue := lrp.ToJSON()

	lrp.State = models.TransitionalLRPStateRunning
	changedValue := lrp.ToJSON()

	return retryIndefinitelyOnStoreTimeout(func() error {
		return self.store.CompareAndSwap(storeadapter.StoreNode{
			Key:   transitionalLongRunningProcessSchemaPath(lrp),
			Value: originalValue,
		}, storeadapter.StoreNode{
			Key:   transitionalLongRunningProcessSchemaPath(lrp),
			Value: changedValue,
		})
	})
}

// The executor calls this when it has finished running the task (be it success or failure)
// stagerBBS will retry this repeatedly if it gets a StoreTimeout error (up to N seconds?)
// This really really shouldn't fail.  If it does, blog about it and walk away. If it failed in a
// consistent way (i.e. key already exists), there's probably a flaw in our design.
func (self *executorBBS) CompleteTask(task models.Task, failed bool, failureReason string, result string) (models.Task, error) {
	originalValue := task.ToJSON()

	task.UpdatedAt = self.timeProvider.Time().UnixNano()

	task.State = models.TaskStateCompleted
	task.Failed = failed
	task.FailureReason = failureReason
	task.Result = result

	err := retryIndefinitelyOnStoreTimeout(func() error {
		return self.store.CompareAndSwap(storeadapter.StoreNode{
			Key:   taskSchemaPath(task),
			Value: originalValue,
		}, storeadapter.StoreNode{
			Key:   taskSchemaPath(task),
			Value: task.ToJSON(),
		})
	})
	return task, err
}

type compareAndSwappableTask struct {
	OldIndex uint64
	NewTask  models.Task
}

// ConvergeTask is run by *one* executor every X seconds (doesn't really matter what X is.. pick something performant)
// Converge will:
// 1. Kick (by setting) any run-onces that are still pending
// 2. Kick (by setting) any run-onces that are completed
// 3. Demote to pending any claimed run-onces that have been claimed for > 30s
// 4. Demote to completed any resolving run-onces that have been resolving for > 30s
// 5. Mark as failed any run-onces that have been in the pending state for > timeToClaim
// 6. Mark as failed any claimed or running run-onces whose executor has stopped maintaining presence
func (self *executorBBS) ConvergeTask(timeToClaim time.Duration) {
	taskState, err := self.store.ListRecursively(TaskSchemaRoot)
	if err != nil {
		return
	}

	executorState, err := self.store.ListRecursively(ExecutorSchemaRoot)
	if err == storeadapter.ErrorKeyNotFound {
		executorState = storeadapter.StoreNode{}
	} else if err != nil {
		return
	}

	logger := gosteno.NewLogger("bbs")
	logError := func(task models.Task, message string) {
		logger.Errord(map[string]interface{}{
			"task": task,
		}, message)
	}

	keysToDelete := []string{}
	unclaimedTimeoutBoundary := self.timeProvider.Time().Add(-timeToClaim).UnixNano()

	tasksToCAS := []compareAndSwappableTask{}
	scheduleForCASByIndex := func(index uint64, newTask models.Task) {
		tasksToCAS = append(tasksToCAS, compareAndSwappableTask{
			OldIndex: index,
			NewTask:  newTask,
		})
	}

	for _, node := range taskState.ChildNodes {
		task, err := models.NewTaskFromJSON(node.Value)
		if err != nil {
			logger.Errord(map[string]interface{}{
				"key":   node.Key,
				"value": string(node.Value),
			}, "task.converge.json-parse-failure")
			keysToDelete = append(keysToDelete, node.Key)
			continue
		}

		switch task.State {
		case models.TaskStatePending:
			if task.CreatedAt <= unclaimedTimeoutBoundary {
				logError(task, "task.converge.failed-to-claim")
				scheduleForCASByIndex(node.Index, markTaskFailed(task, "not claimed within time limit"))
			} else {
				scheduleForCASByIndex(node.Index, task)
			}
		case models.TaskStateClaimed:
			claimedTooLong := self.timeProvider.Time().Sub(time.Unix(0, task.UpdatedAt)) >= 30*time.Second
			_, executorIsAlive := executorState.Lookup(task.ExecutorID)

			if !executorIsAlive {
				logError(task, "task.converge.executor-disappeared")
				scheduleForCASByIndex(node.Index, markTaskFailed(task, "executor disappeared before completion"))
			} else if claimedTooLong {
				logError(task, "task.converge.failed-to-start")
				scheduleForCASByIndex(node.Index, demoteToPending(task))
			}
		case models.TaskStateRunning:
			_, executorIsAlive := executorState.Lookup(task.ExecutorID)

			if !executorIsAlive {
				logError(task, "task.converge.executor-disappeared")
				scheduleForCASByIndex(node.Index, markTaskFailed(task, "executor disappeared before completion"))
			}
		case models.TaskStateCompleted:
			scheduleForCASByIndex(node.Index, task)
		case models.TaskStateResolving:
			resolvingTooLong := self.timeProvider.Time().Sub(time.Unix(0, task.UpdatedAt)) >= 30*time.Second

			if resolvingTooLong {
				logError(task, "task.converge.failed-to-resolve")
				scheduleForCASByIndex(node.Index, demoteToCompleted(task))
			}
		}
	}

	self.batchCompareAndSwapTasks(tasksToCAS, logger)
	self.store.Delete(keysToDelete...)
}

func (self *executorBBS) batchCompareAndSwapTasks(tasksToCAS []compareAndSwappableTask, logger *gosteno.Logger) {
	done := make(chan struct{}, len(tasksToCAS))

	for _, taskToCAS := range tasksToCAS {
		task := taskToCAS.NewTask
		task.UpdatedAt = self.timeProvider.Time().UnixNano()
		newStoreNode := storeadapter.StoreNode{
			Key:   taskSchemaPath(task),
			Value: task.ToJSON(),
		}

		go func() {
			err := self.store.CompareAndSwapByIndex(taskToCAS.OldIndex, newStoreNode)
			if err != nil {
				logger.Errord(map[string]interface{}{
					"error": err.Error(),
				}, "task.converge.failed-to-compare-and-swap")
			}
			done <- struct{}{}
		}()
	}

	for _ = range tasksToCAS {
		<-done
	}
}

func markTaskFailed(task models.Task, reason string) models.Task {
	task.State = models.TaskStateCompleted
	task.Failed = true
	task.FailureReason = reason
	return task
}

func demoteToPending(task models.Task) models.Task {
	task.State = models.TaskStatePending
	task.ExecutorID = ""
	task.ContainerHandle = ""
	return task
}

func demoteToCompleted(task models.Task) models.Task {
	task.State = models.TaskStateCompleted
	return task
}

func (self *executorBBS) MaintainConvergeLock(interval time.Duration, executorID string) (<-chan bool, chan<- chan bool, error) {
	return self.store.MaintainNode(storeadapter.StoreNode{
		Key:   lockSchemaPath("converge_lock"),
		Value: []byte(executorID),
		TTL:   uint64(interval.Seconds()),
	})
}
