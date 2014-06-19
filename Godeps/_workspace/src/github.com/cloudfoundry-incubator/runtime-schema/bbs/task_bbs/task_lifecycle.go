package task_bbs

import (
	"errors"
	"fmt"

	"github.com/cloudfoundry-incubator/runtime-schema/bbs/shared"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/storeadapter"
)

// The stager calls this when it wants to desire a payload
// stagerTaskBBS will retry this repeatedly if it gets a StoreTimeout error (up to N seconds?)
// If this fails, the stager should bail and run its "this-failed-to-stage" routine
func (s *TaskBBS) DesireTask(task models.Task) error {
	err := shared.RetryIndefinitelyOnStoreTimeout(func() error {
		if task.CreatedAt == 0 {
			task.CreatedAt = s.timeProvider.Time().UnixNano()
		}
		task.UpdatedAt = s.timeProvider.Time().UnixNano()
		task.State = models.TaskStatePending
		return s.store.Create(storeadapter.StoreNode{
			Key:   shared.TaskSchemaPath(task.Guid),
			Value: task.ToJSON(),
		})
	})
	return err
}

// The executor calls this when it wants to claim a task
// stagerTaskBBS will retry this repeatedly if it gets a StoreTimeout error (up to N seconds?)
// If this fails, the executor should assume that someone else is handling the claim and should bail
func (bbs *TaskBBS) ClaimTask(taskGuid string, executorID string) error {
	task, index, err := bbs.getTask(taskGuid)

	if err != nil {
		return fmt.Errorf("cannot claim non-existing task: %s", err.Error())
	}

	if task.State != models.TaskStatePending {
		return errors.New("cannot claim task in non-pending state")
	}

	task.UpdatedAt = bbs.timeProvider.Time().UnixNano()
	task.State = models.TaskStateClaimed
	task.ExecutorID = executorID

	return shared.RetryIndefinitelyOnStoreTimeout(func() error {
		return bbs.store.CompareAndSwapByIndex(index, storeadapter.StoreNode{
			Key:   shared.TaskSchemaPath(taskGuid),
			Value: task.ToJSON(),
		})
	})
}

// The executor calls this when it is about to run the task in the claimed container
// stagerTaskBBS will retry this repeatedly if it gets a StoreTimeout error (up to N seconds?)
// If this fails, the executor should assume that someone else is running and should clean up and bail
func (bbs *TaskBBS) StartTask(taskGuid string, executorID string, containerHandle string) error {
	task, index, err := bbs.getTask(taskGuid)

	if err != nil {
		return fmt.Errorf("cannot start non-existing task: %s", err.Error())
	}

	if task.State != models.TaskStateClaimed {
		return errors.New("cannot start task in non-claimed state")
	}

	if task.ExecutorID != executorID {
		return errors.New("cannot start task claimed by another executor")
	}

	task.UpdatedAt = bbs.timeProvider.Time().UnixNano()
	task.State = models.TaskStateRunning
	task.ContainerHandle = containerHandle

	return shared.RetryIndefinitelyOnStoreTimeout(func() error {
		return bbs.store.CompareAndSwapByIndex(index, storeadapter.StoreNode{
			Key:   shared.TaskSchemaPath(taskGuid),
			Value: task.ToJSON(),
		})
	})
}

// The executor calls this when it has finished running the task (be it success or failure)
// stagerTaskBBS will retry this repeatedly if it gets a StoreTimeout error (up to N seconds?)
// This really really shouldn't fail.  If it does, blog about it and walk away. If it failed in a
// consistent way (i.e. key already exists), there's probably a flaw in our design.
func (bbs *TaskBBS) CompleteTask(taskGuid string, failed bool, failureReason string, result string) error {
	task, index, err := bbs.getTask(taskGuid)

	if err != nil {
		return fmt.Errorf("cannot complete non-existing task: %s", err.Error())
	}

	if task.State != models.TaskStateRunning {
		return errors.New("cannot complete task in non-running state")
	}

	task.UpdatedAt = bbs.timeProvider.Time().UnixNano()
	task.State = models.TaskStateCompleted
	task.Failed = failed
	task.FailureReason = failureReason
	task.Result = result

	return shared.RetryIndefinitelyOnStoreTimeout(func() error {
		return bbs.store.CompareAndSwapByIndex(index, storeadapter.StoreNode{
			Key:   shared.TaskSchemaPath(taskGuid),
			Value: task.ToJSON(),
		})
	})
}

// The stager calls this when it wants to claim a completed task.  This ensures that only one
// stager ever attempts to handle a completed task
func (bbs *TaskBBS) ResolvingTask(taskGuid string) error {
	task, index, err := bbs.getTask(taskGuid)

	if err != nil {
		return fmt.Errorf("cannot start resolving non-existing task: %s", err.Error())
	}

	if task.State != models.TaskStateCompleted {
		return errors.New("cannot start resolving task in non-completed state")
	}

	task.UpdatedAt = bbs.timeProvider.Time().UnixNano()
	task.State = models.TaskStateResolving

	return shared.RetryIndefinitelyOnStoreTimeout(func() error {
		return bbs.store.CompareAndSwapByIndex(index, storeadapter.StoreNode{
			Key:   shared.TaskSchemaPath(taskGuid),
			Value: task.ToJSON(),
		})
	})
}

// The stager calls this when it wants to signal that it has received a completion and is handling it
// stagerTaskBBS will retry this repeatedly if it gets a StoreTimeout error (up to N seconds?)
// If this fails, the stager should assume that someone else is handling the completion and should bail
func (bbs *TaskBBS) ResolveTask(taskGuid string) error {
	task, _, err := bbs.getTask(taskGuid)

	if err != nil {
		return fmt.Errorf("cannot resolve non-existing task: %s", err.Error())
	}

	if task.State != models.TaskStateResolving {
		return errors.New("cannot resolve task in non-resolving state")
	}

	return shared.RetryIndefinitelyOnStoreTimeout(func() error {
		return bbs.store.Delete(shared.TaskSchemaPath(taskGuid))
	})
}

func (bbs *TaskBBS) getTask(taskGuid string) (models.Task, uint64, error) {
	var node storeadapter.StoreNode
	err := shared.RetryIndefinitelyOnStoreTimeout(func() error {
		var err error
		node, err = bbs.store.Get(shared.TaskSchemaPath(taskGuid))
		return err
	})

	if err != nil {
		return models.Task{}, 0, err
	}

	task, err := models.NewTaskFromJSON(node.Value)

	return task, node.Index, err
}
