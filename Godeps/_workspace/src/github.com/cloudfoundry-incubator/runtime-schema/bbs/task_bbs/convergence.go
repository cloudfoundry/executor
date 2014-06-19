package task_bbs

import (
	"sync"
	"time"

	"github.com/cloudfoundry-incubator/runtime-schema/bbs/shared"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/storeadapter"
)

type compareAndSwappableTask struct {
	OldIndex uint64
	NewTask  models.Task
}

// ConvergeTask is run by *one* executor every X seconds (doesn't really matter what X is.. pick something performant)
// Converge will:
// 1. Kick (by setting) any run-onces that are still pending (and have been for > convergence interval)
// 2. Kick (by setting) any run-onces that are completed (and have been for > convergence interval)
// 3. Demote to pending any claimed run-onces that have been claimed for > 30s
// 4. Demote to completed any resolving run-onces that have been resolving for > 30s
// 5. Mark as failed any run-onces that have been in the pending state for > timeToClaim
// 6. Mark as failed any claimed or running run-onces whose executor has stopped maintaining presence
func (bbs *TaskBBS) ConvergeTask(timeToClaim time.Duration, convergenceInterval time.Duration) {
	taskState, err := bbs.store.ListRecursively(shared.TaskSchemaRoot)
	if err != nil {
		return
	}

	executorState, err := bbs.store.ListRecursively(shared.ExecutorSchemaRoot)
	if err == storeadapter.ErrorKeyNotFound {
		executorState = storeadapter.StoreNode{}
	} else if err != nil {
		return
	}

	logError := func(task models.Task, message string) {
		bbs.logger.Errord(map[string]interface{}{
			"task": task,
		}, message)
	}

	keysToDelete := []string{}

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
			bbs.logger.Errord(map[string]interface{}{
				"key":   node.Key,
				"value": string(node.Value),
			}, "task.converge.json-parse-failure")
			keysToDelete = append(keysToDelete, node.Key)
			continue
		}

		shouldKickTask := bbs.durationSinceTaskUpdated(task) >= convergenceInterval

		switch task.State {
		case models.TaskStatePending:
			shouldMarkAsFailed := bbs.durationSinceTaskCreated(task) >= timeToClaim
			if shouldMarkAsFailed {
				logError(task, "task.converge.failed-to-claim")
				scheduleForCASByIndex(node.Index, markTaskFailed(task, "not claimed within time limit"))
			} else if shouldKickTask {
				scheduleForCASByIndex(node.Index, task)
			}
		case models.TaskStateClaimed:
			_, executorIsAlive := executorState.Lookup(task.ExecutorID)

			if !executorIsAlive {
				logError(task, "task.converge.executor-disappeared")
				scheduleForCASByIndex(node.Index, markTaskFailed(task, "executor disappeared before completion"))
			} else if shouldKickTask {
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
			if shouldKickTask {
				scheduleForCASByIndex(node.Index, task)
			}
		case models.TaskStateResolving:
			if shouldKickTask {
				logError(task, "task.converge.failed-to-resolve")
				scheduleForCASByIndex(node.Index, demoteToCompleted(task))
			}
		}
	}

	bbs.batchCompareAndSwapTasks(tasksToCAS)
	bbs.store.Delete(keysToDelete...)
}

func (bbs *TaskBBS) durationSinceTaskCreated(task models.Task) time.Duration {
	return bbs.timeProvider.Time().Sub(time.Unix(0, task.CreatedAt))
}

func (bbs *TaskBBS) durationSinceTaskUpdated(task models.Task) time.Duration {
	return bbs.timeProvider.Time().Sub(time.Unix(0, task.UpdatedAt))
}

func markTaskFailed(task models.Task, reason string) models.Task {
	task.State = models.TaskStateCompleted
	task.Failed = true
	task.FailureReason = reason
	return task
}

func (bbs *TaskBBS) batchCompareAndSwapTasks(tasksToCAS []compareAndSwappableTask) {
	waitGroup := &sync.WaitGroup{}
	waitGroup.Add(len(tasksToCAS))
	for _, taskToCAS := range tasksToCAS {
		task := taskToCAS.NewTask
		task.UpdatedAt = bbs.timeProvider.Time().UnixNano()
		newStoreNode := storeadapter.StoreNode{
			Key:   shared.TaskSchemaPath(task.Guid),
			Value: task.ToJSON(),
		}

		go func(taskToCAS compareAndSwappableTask, newStoreNode storeadapter.StoreNode) {
			err := bbs.store.CompareAndSwapByIndex(taskToCAS.OldIndex, newStoreNode)
			if err != nil {
				bbs.logger.Errord(map[string]interface{}{
					"error": err.Error(),
				}, "task.converge.failed-to-compare-and-swap")
			}
			waitGroup.Done()
		}(taskToCAS, newStoreNode)
	}

	waitGroup.Wait()
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
