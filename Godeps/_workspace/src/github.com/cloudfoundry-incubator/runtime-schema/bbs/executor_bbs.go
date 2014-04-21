package bbs

import (
	"github.com/cloudfoundry/gosteno"
	"github.com/cloudfoundry/gunk/timeprovider"
	"time"

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

func (self *executorBBS) WatchForDesiredTask() (<-chan *models.Task, chan<- bool, <-chan error) {
	return watchForTaskModificationsOnState(self.store, models.TaskStatePending)
}

// The executor calls this when it wants to claim a task
// stagerBBS will retry this repeatedly if it gets a StoreTimeout error (up to N seconds?)
// If this fails, the executor should assume that someone else is handling the claim and should bail
func (self *executorBBS) ClaimTask(task *models.Task, executorID string) error {
	originalValue := task.ToJSON()

	task.UpdatedAt = self.timeProvider.Time().UnixNano()

	task.State = models.TaskStateClaimed
	task.ExecutorID = executorID

	return retryIndefinitelyOnStoreTimeout(func() error {
		return self.store.CompareAndSwap(storeadapter.StoreNode{
			Key:   taskSchemaPath(task),
			Value: originalValue,
		}, storeadapter.StoreNode{
			Key:   taskSchemaPath(task),
			Value: task.ToJSON(),
		})
	})
}

// The executor calls this when it is about to run the task in the claimed container
// stagerBBS will retry this repeatedly if it gets a StoreTimeout error (up to N seconds?)
// If this fails, the executor should assume that someone else is running and should clean up and bail
func (self *executorBBS) StartTask(task *models.Task, containerHandle string) error {
	originalValue := task.ToJSON()

	task.UpdatedAt = self.timeProvider.Time().UnixNano()

	task.State = models.TaskStateRunning
	task.ContainerHandle = containerHandle

	return retryIndefinitelyOnStoreTimeout(func() error {
		return self.store.CompareAndSwap(storeadapter.StoreNode{
			Key:   taskSchemaPath(task),
			Value: originalValue,
		}, storeadapter.StoreNode{
			Key:   taskSchemaPath(task),
			Value: task.ToJSON(),
		})
	})
}

// The executor calls this when it has finished running the task (be it success or failure)
// stagerBBS will retry this repeatedly if it gets a StoreTimeout error (up to N seconds?)
// This really really shouldn't fail.  If it does, blog about it and walk away. If it failed in a
// consistent way (i.e. key already exists), there's probably a flaw in our design.
func (self *executorBBS) CompleteTask(task *models.Task, failed bool, failureReason string, result string) error {
	originalValue := task.ToJSON()

	task.UpdatedAt = self.timeProvider.Time().UnixNano()

	task.State = models.TaskStateCompleted
	task.Failed = failed
	task.FailureReason = failureReason
	task.Result = result

	return retryIndefinitelyOnStoreTimeout(func() error {
		return self.store.CompareAndSwap(storeadapter.StoreNode{
			Key:   taskSchemaPath(task),
			Value: originalValue,
		}, storeadapter.StoreNode{
			Key:   taskSchemaPath(task),
			Value: task.ToJSON(),
		})
	})
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

	tasksToCAS := [][]models.Task{}
	scheduleForCAS := func(oldTask, newTask models.Task) {
		tasksToCAS = append(tasksToCAS, []models.Task{
			oldTask,
			newTask,
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
				scheduleForCAS(task, markTaskFailed(task, "not claimed within time limit"))
			} else {
				scheduleForCAS(task, task)
			}
		case models.TaskStateClaimed:
			claimedTooLong := self.timeProvider.Time().Sub(time.Unix(0, task.UpdatedAt)) >= 30*time.Second
			_, executorIsAlive := executorState.Lookup(task.ExecutorID)

			if !executorIsAlive {
				logError(task, "task.converge.executor-disappeared")
				scheduleForCAS(task, markTaskFailed(task, "executor disappeared before completion"))
			} else if claimedTooLong {
				logError(task, "task.converge.failed-to-start")
				scheduleForCAS(task, demoteToPending(task))
			}
		case models.TaskStateRunning:
			_, executorIsAlive := executorState.Lookup(task.ExecutorID)

			if !executorIsAlive {
				logError(task, "task.converge.executor-disappeared")
				scheduleForCAS(task, markTaskFailed(task, "executor disappeared before completion"))
			}
		case models.TaskStateCompleted:
			scheduleForCAS(task, task)
		case models.TaskStateResolving:
			resolvingTooLong := self.timeProvider.Time().Sub(time.Unix(0, task.UpdatedAt)) >= 30*time.Second

			if resolvingTooLong {
				logError(task, "task.converge.failed-to-resolve")
				scheduleForCAS(task, demoteToCompleted(task))
			}
		}
	}

	self.batchCompareAndSwapTasks(tasksToCAS, logger)
	self.store.Delete(keysToDelete...)
}

func (self *executorBBS) batchCompareAndSwapTasks(tasksToCAS [][]models.Task, logger *gosteno.Logger) {
	done := make(chan struct{}, len(tasksToCAS))

	for _, taskPair := range tasksToCAS {
		originalStoreNode := storeadapter.StoreNode{
			Key:   taskSchemaPath(&taskPair[0]),
			Value: taskPair[0].ToJSON(),
		}

		taskPair[1].UpdatedAt = self.timeProvider.Time().UnixNano()
		newStoreNode := storeadapter.StoreNode{
			Key:   taskSchemaPath(&taskPair[1]),
			Value: taskPair[1].ToJSON(),
		}

		go func() {
			err := self.store.CompareAndSwap(originalStoreNode, newStoreNode)
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
