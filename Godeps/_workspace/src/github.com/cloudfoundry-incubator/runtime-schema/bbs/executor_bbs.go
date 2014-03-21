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

func (self *executorBBS) WatchForDesiredRunOnce() (<-chan *models.RunOnce, chan<- bool, <-chan error) {
	return watchForRunOnceModificationsOnState(self.store, models.RunOnceStatePending)
}

// The executor calls this when it wants to claim a runonce
// stagerBBS will retry this repeatedly if it gets a StoreTimeout error (up to N seconds?)
// If this fails, the executor should assume that someone else is handling the claim and should bail
func (self *executorBBS) ClaimRunOnce(runOnce *models.RunOnce, executorID string) error {
	originalValue := runOnce.ToJSON()

	runOnce.UpdatedAt = self.timeProvider.Time().UnixNano()

	runOnce.State = models.RunOnceStateClaimed
	runOnce.ExecutorID = executorID

	return retryIndefinitelyOnStoreTimeout(func() error {
		return self.store.CompareAndSwap(storeadapter.StoreNode{
			Key:   runOnceSchemaPath(runOnce),
			Value: originalValue,
		}, storeadapter.StoreNode{
			Key:   runOnceSchemaPath(runOnce),
			Value: runOnce.ToJSON(),
		})
	})
}

// The executor calls this when it is about to run the runonce in the claimed container
// stagerBBS will retry this repeatedly if it gets a StoreTimeout error (up to N seconds?)
// If this fails, the executor should assume that someone else is running and should clean up and bail
func (self *executorBBS) StartRunOnce(runOnce *models.RunOnce, containerHandle string) error {
	originalValue := runOnce.ToJSON()

	runOnce.UpdatedAt = self.timeProvider.Time().UnixNano()

	runOnce.State = models.RunOnceStateRunning
	runOnce.ContainerHandle = containerHandle

	return retryIndefinitelyOnStoreTimeout(func() error {
		return self.store.CompareAndSwap(storeadapter.StoreNode{
			Key:   runOnceSchemaPath(runOnce),
			Value: originalValue,
		}, storeadapter.StoreNode{
			Key:   runOnceSchemaPath(runOnce),
			Value: runOnce.ToJSON(),
		})
	})
}

// The executor calls this when it has finished running the runonce (be it success or failure)
// stagerBBS will retry this repeatedly if it gets a StoreTimeout error (up to N seconds?)
// This really really shouldn't fail.  If it does, blog about it and walk away. If it failed in a
// consistent way (i.e. key already exists), there's probably a flaw in our design.
func (self *executorBBS) CompleteRunOnce(runOnce *models.RunOnce, failed bool, failureReason string, result string) error {
	originalValue := runOnce.ToJSON()

	runOnce.UpdatedAt = self.timeProvider.Time().UnixNano()

	runOnce.State = models.RunOnceStateCompleted
	runOnce.Failed = failed
	runOnce.FailureReason = failureReason
	runOnce.Result = result

	return retryIndefinitelyOnStoreTimeout(func() error {
		return self.store.CompareAndSwap(storeadapter.StoreNode{
			Key:   runOnceSchemaPath(runOnce),
			Value: originalValue,
		}, storeadapter.StoreNode{
			Key:   runOnceSchemaPath(runOnce),
			Value: runOnce.ToJSON(),
		})
	})
}

// ConvergeRunOnce is run by *one* executor every X seconds (doesn't really matter what X is.. pick something performant)
// Converge will:
// 1. Kick (by setting) any run-onces that are still pending
// 2. Kick (by setting) any run-onces that are completed
// 3. Demote to pending any claimed run-onces that have been claimed for > 30s
// 4. Demote to completed any resolving run-onces that have been resolving for > 30s
// 5. Mark as failed any run-onces that have been in the pending state for > timeToClaim
// 6. Mark as failed any claimed or running run-onces whose executor has stopped maintaining presence
func (self *executorBBS) ConvergeRunOnce(timeToClaim time.Duration) {
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

	logger := gosteno.NewLogger("bbs")
	logError := func(runOnce models.RunOnce, message string) {
		logger.Errord(map[string]interface{}{
			"runonce": runOnce,
		}, message)
	}

	keysToDelete := []string{}
	unclaimedTimeoutBoundary := self.timeProvider.Time().Add(-timeToClaim).UnixNano()

	runOncesToCAS := [][]models.RunOnce{}
	scheduleForCAS := func(oldRunOnce, newRunOnce models.RunOnce) {
		runOncesToCAS = append(runOncesToCAS, []models.RunOnce{
			oldRunOnce,
			newRunOnce,
		})
	}

	for _, node := range runOnceState.ChildNodes {
		runOnce, err := models.NewRunOnceFromJSON(node.Value)
		if err != nil {
			logger.Errord(map[string]interface{}{
				"key":   node.Key,
				"value": string(node.Value),
			}, "runonce.converge.json-parse-failure")
			keysToDelete = append(keysToDelete, node.Key)
			continue
		}

		switch runOnce.State {
		case models.RunOnceStatePending:
			if runOnce.CreatedAt <= unclaimedTimeoutBoundary {
				logError(runOnce, "runonce.converge.failed-to-claim")
				scheduleForCAS(runOnce, markRunOnceFailed(runOnce, "not claimed within time limit"))
			} else {
				scheduleForCAS(runOnce, runOnce)
			}
		case models.RunOnceStateClaimed:
			claimedTooLong := self.timeProvider.Time().Sub(time.Unix(0, runOnce.UpdatedAt)) >= 30*time.Second
			_, executorIsAlive := executorState.Lookup(runOnce.ExecutorID)

			if !executorIsAlive {
				logError(runOnce, "runonce.converge.executor-disappeared")
				scheduleForCAS(runOnce, markRunOnceFailed(runOnce, "executor disappeared before completion"))
			} else if claimedTooLong {
				logError(runOnce, "runonce.converge.failed-to-start")
				scheduleForCAS(runOnce, demoteToPending(runOnce))
			}
		case models.RunOnceStateRunning:
			_, executorIsAlive := executorState.Lookup(runOnce.ExecutorID)

			if !executorIsAlive {
				logError(runOnce, "runonce.converge.executor-disappeared")
				scheduleForCAS(runOnce, markRunOnceFailed(runOnce, "executor disappeared before completion"))
			}
		case models.RunOnceStateCompleted:
			scheduleForCAS(runOnce, runOnce)
		case models.RunOnceStateResolving:
			resolvingTooLong := self.timeProvider.Time().Sub(time.Unix(0, runOnce.UpdatedAt)) >= 30*time.Second

			if resolvingTooLong {
				logError(runOnce, "runonce.converge.failed-to-resolve")
				scheduleForCAS(runOnce, demoteToCompleted(runOnce))
			}
		}
	}

	self.batchCompareAndSwapRunOnces(runOncesToCAS, logger)
	self.store.Delete(keysToDelete...)
}

func (self *executorBBS) batchCompareAndSwapRunOnces(runOncesToCAS [][]models.RunOnce, logger *gosteno.Logger) {
	done := make(chan struct{}, len(runOncesToCAS))

	for _, runOncePair := range runOncesToCAS {
		originalStoreNode := storeadapter.StoreNode{
			Key:   runOnceSchemaPath(&runOncePair[0]),
			Value: runOncePair[0].ToJSON(),
		}

		runOncePair[1].UpdatedAt = self.timeProvider.Time().UnixNano()
		newStoreNode := storeadapter.StoreNode{
			Key:   runOnceSchemaPath(&runOncePair[1]),
			Value: runOncePair[1].ToJSON(),
		}

		go func() {
			err := self.store.CompareAndSwap(originalStoreNode, newStoreNode)
			if err != nil {
				logger.Errord(map[string]interface{}{
					"error": err.Error(),
				}, "runonce.converge.failed-to-compare-and-swap")
			}
			done <- struct{}{}
		}()
	}

	for _ = range runOncesToCAS {
		<-done
	}
}

func markRunOnceFailed(runOnce models.RunOnce, reason string) models.RunOnce {
	runOnce.State = models.RunOnceStateCompleted
	runOnce.Failed = true
	runOnce.FailureReason = reason
	return runOnce
}

func demoteToPending(runOnce models.RunOnce) models.RunOnce {
	runOnce.State = models.RunOnceStatePending
	runOnce.ExecutorID = ""
	runOnce.ContainerHandle = ""
	return runOnce
}

func demoteToCompleted(runOnce models.RunOnce) models.RunOnce {
	runOnce.State = models.RunOnceStateCompleted
	return runOnce
}

func (self *executorBBS) MaintainConvergeLock(interval time.Duration, executorID string) (<-chan bool, chan<- chan bool, error) {
	return self.store.MaintainNode(storeadapter.StoreNode{
		Key:   lockSchemaPath("converge_lock"),
		Value: []byte(executorID),
		TTL:   uint64(interval.Seconds()),
	})
}
