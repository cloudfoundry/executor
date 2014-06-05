package task_bbs

import (
	"github.com/cloudfoundry-incubator/runtime-schema/bbs/shared"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/storeadapter"
)

// The stager calls this when it wants to desire a payload
// stagerTaskBBS will retry this repeatedly if it gets a StoreTimeout error (up to N seconds?)
// If this fails, the stager should bail and run its "this-failed-to-stage" routine
func (s *TaskBBS) DesireTask(task models.Task) (models.Task, error) {
	err := shared.RetryIndefinitelyOnStoreTimeout(func() error {
		if task.CreatedAt == 0 {
			task.CreatedAt = s.timeProvider.Time().UnixNano()
		}
		task.UpdatedAt = s.timeProvider.Time().UnixNano()
		task.State = models.TaskStatePending
		return s.store.Create(storeadapter.StoreNode{
			Key:   shared.TaskSchemaPath(task),
			Value: task.ToJSON(),
		})
	})
	return task, err
}

// The executor calls this when it wants to claim a task
// stagerTaskBBS will retry this repeatedly if it gets a StoreTimeout error (up to N seconds?)
// If this fails, the executor should assume that someone else is handling the claim and should bail
func (bbs *TaskBBS) ClaimTask(task models.Task, executorID string) (models.Task, error) {
	originalValue := task.ToJSON()

	task.UpdatedAt = bbs.timeProvider.Time().UnixNano()

	task.State = models.TaskStateClaimed
	task.ExecutorID = executorID

	err := shared.RetryIndefinitelyOnStoreTimeout(func() error {
		return bbs.store.CompareAndSwap(storeadapter.StoreNode{
			Key:   shared.TaskSchemaPath(task),
			Value: originalValue,
		}, storeadapter.StoreNode{
			Key:   shared.TaskSchemaPath(task),
			Value: task.ToJSON(),
		})
	})

	return task, err
}

// The executor calls this when it is about to run the task in the claimed container
// stagerTaskBBS will retry this repeatedly if it gets a StoreTimeout error (up to N seconds?)
// If this fails, the executor should assume that someone else is running and should clean up and bail
func (bbs *TaskBBS) StartTask(task models.Task, containerHandle string) (models.Task, error) {
	originalValue := task.ToJSON()

	task.UpdatedAt = bbs.timeProvider.Time().UnixNano()

	task.State = models.TaskStateRunning
	task.ContainerHandle = containerHandle

	err := shared.RetryIndefinitelyOnStoreTimeout(func() error {
		return bbs.store.CompareAndSwap(storeadapter.StoreNode{
			Key:   shared.TaskSchemaPath(task),
			Value: originalValue,
		}, storeadapter.StoreNode{
			Key:   shared.TaskSchemaPath(task),
			Value: task.ToJSON(),
		})
	})

	return task, err
}

// The executor calls this when it has finished running the task (be it success or failure)
// stagerTaskBBS will retry this repeatedly if it gets a StoreTimeout error (up to N seconds?)
// This really really shouldn't fail.  If it does, blog about it and walk away. If it failed in a
// consistent way (i.e. key already exists), there's probably a flaw in our design.
func (bbs *TaskBBS) CompleteTask(task models.Task, failed bool, failureReason string, result string) (models.Task, error) {
	originalValue := task.ToJSON()

	task.UpdatedAt = bbs.timeProvider.Time().UnixNano()

	task.State = models.TaskStateCompleted
	task.Failed = failed
	task.FailureReason = failureReason
	task.Result = result

	err := shared.RetryIndefinitelyOnStoreTimeout(func() error {
		return bbs.store.CompareAndSwap(storeadapter.StoreNode{
			Key:   shared.TaskSchemaPath(task),
			Value: originalValue,
		}, storeadapter.StoreNode{
			Key:   shared.TaskSchemaPath(task),
			Value: task.ToJSON(),
		})
	})
	return task, err
}

// The stager calls this when it wants to claim a completed task.  This ensures that only one
// stager ever attempts to handle a completed task
func (s *TaskBBS) ResolvingTask(task models.Task) (models.Task, error) {
	originalValue := task.ToJSON()

	task.UpdatedAt = s.timeProvider.Time().UnixNano()
	task.State = models.TaskStateResolving

	err := shared.RetryIndefinitelyOnStoreTimeout(func() error {
		return s.store.CompareAndSwap(storeadapter.StoreNode{
			Key:   shared.TaskSchemaPath(task),
			Value: originalValue,
		}, storeadapter.StoreNode{
			Key:   shared.TaskSchemaPath(task),
			Value: task.ToJSON(),
		})
	})
	return task, err
}

// The stager calls this when it wants to signal that it has received a completion and is handling it
// stagerTaskBBS will retry this repeatedly if it gets a StoreTimeout error (up to N seconds?)
// If this fails, the stager should assume that someone else is handling the completion and should bail
func (s *TaskBBS) ResolveTask(task models.Task) (models.Task, error) {
	err := shared.RetryIndefinitelyOnStoreTimeout(func() error {
		return s.store.Delete(shared.TaskSchemaPath(task))
	})
	return task, err
}
