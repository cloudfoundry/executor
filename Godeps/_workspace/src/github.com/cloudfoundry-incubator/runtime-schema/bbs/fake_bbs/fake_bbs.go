package fake_bbs

import (
	"errors"
	"github.com/cloudfoundry-incubator/runtime-schema/bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"

	"time"
)

type FakePresence struct {
	MaintainStatus bool
	Maintained     bool
	Removed        bool
	ticker         *time.Ticker
	done           chan struct{}
}

func (p *FakePresence) Maintain(interval time.Duration) (<-chan bool, error) {
	if p.ticker != nil {
		return nil, errors.New("Already being maintained")
	}

	status := make(chan bool)
	p.done = make(chan struct{})
	p.ticker = time.NewTicker(interval)
	go func() {
		status <- p.MaintainStatus
		for {
			select {
			case <-p.ticker.C:
				status <- p.MaintainStatus
			case <-p.done:
				close(status)
				return
			}
		}
	}()
	p.Maintained = true
	return status, nil
}

func (p *FakePresence) Remove() {
	p.ticker.Stop()
	p.ticker = nil
	close(p.done)

	p.Removed = true
}

type FakeExecutorBBS struct {
	CallsToConverge int

	MaintainConvergeInterval      time.Duration
	MaintainConvergeExecutorID    string
	MaintainConvergeStatusChannel <-chan bool
	MaintainConvergeStopChannel   chan<- chan bool
	MaintainConvergeLockError     error

	MaintainingPresenceHeartbeatInterval time.Duration
	MaintainingPresenceExecutorID        string
	MaintainingPresencePresence          *FakePresence
	MaintainingPresenceError             error

	ClaimedRunOnce  *models.RunOnce
	ClaimRunOnceErr error

	StartedRunOnce  *models.RunOnce
	StartRunOnceErr error

	CompletedRunOnce           *models.RunOnce
	CompleteRunOnceErr         error
	ConvergeRunOnceTimeToClaim time.Duration
}

func NewFakeExecutorBBS() *FakeExecutorBBS {
	return &FakeExecutorBBS{}
}

func (fakeBBS *FakeExecutorBBS) MaintainExecutorPresence(heartbeatInterval time.Duration, executorID string) (bbs.Presence, <-chan bool, error) {
	fakeBBS.MaintainingPresenceHeartbeatInterval = heartbeatInterval
	fakeBBS.MaintainingPresenceExecutorID = executorID
	fakeBBS.MaintainingPresencePresence = &FakePresence{}
	status, _ := fakeBBS.MaintainingPresencePresence.Maintain(heartbeatInterval)

	return fakeBBS.MaintainingPresencePresence, status, fakeBBS.MaintainingPresenceError
}

func (fakeBBS *FakeExecutorBBS) WatchForDesiredRunOnce() (<-chan *models.RunOnce, chan<- bool, <-chan error) {
	return nil, nil, nil
}

func (fakeBBS *FakeExecutorBBS) ClaimRunOnce(runOnce *models.RunOnce, executorID string) error {
	runOnce.ExecutorID = executorID
	fakeBBS.ClaimedRunOnce = runOnce
	return fakeBBS.ClaimRunOnceErr
}

func (fakeBBS *FakeExecutorBBS) StartRunOnce(runOnce *models.RunOnce, containerHandle string) error {
	runOnce.ContainerHandle = containerHandle
	fakeBBS.StartedRunOnce = runOnce
	return fakeBBS.StartRunOnceErr
}

func (fakeBBS *FakeExecutorBBS) CompleteRunOnce(runOnce *models.RunOnce, failed bool, failureReason string, result string) error {
	runOnce.Failed = failed
	runOnce.FailureReason = failureReason
	runOnce.Result = result
	fakeBBS.CompletedRunOnce = runOnce
	return fakeBBS.CompleteRunOnceErr
}

func (fakeBBS *FakeExecutorBBS) ConvergeRunOnce(timeToClaim time.Duration) {
	fakeBBS.ConvergeRunOnceTimeToClaim = timeToClaim
	fakeBBS.CallsToConverge++
}

func (fakeBBS *FakeExecutorBBS) MaintainConvergeLock(interval time.Duration, executorID string) (<-chan bool, chan<- chan bool, error) {
	fakeBBS.MaintainConvergeInterval = interval
	fakeBBS.MaintainConvergeExecutorID = executorID
	status := make(chan bool)
	fakeBBS.MaintainConvergeStatusChannel = status
	stop := make(chan chan bool)
	fakeBBS.MaintainConvergeStopChannel = stop

	ticker := time.NewTicker(interval)
	go func() {
		status <- true
		for {
			select {
			case <-ticker.C:
				status <- true
			case release := <-stop:
				ticker.Stop()
				close(status)
				if release != nil {
					close(release)
				}

				return
			}
		}
	}()

	return fakeBBS.MaintainConvergeStatusChannel, fakeBBS.MaintainConvergeStopChannel, fakeBBS.MaintainConvergeLockError
}

func (fakeBBS *FakeExecutorBBS) Stop() {
	if fakeBBS.MaintainingPresencePresence != nil {
		fakeBBS.MaintainingPresencePresence.Remove()
	}

	if fakeBBS.MaintainConvergeStopChannel != nil {
		fakeBBS.MaintainConvergeStopChannel <- nil
	}
}

type FakeStagerBBS struct {
	ResolvedRunOnce   *models.RunOnce
	ResolveRunOnceErr error

	ResolvingRunOnceInput struct {
		RunOnceToResolve *models.RunOnce
	}
	ResolvingRunOnceOutput struct {
		Err error
	}

	CalledCompletedRunOnce  chan bool
	CompletedRunOnceChan    chan *models.RunOnce
	CompletedRunOnceErrChan chan error
}

func NewFakeStagerBBS() *FakeStagerBBS {
	return &FakeStagerBBS{
		CalledCompletedRunOnce: make(chan bool),
	}
}

func (fakeBBS *FakeStagerBBS) WatchForCompletedRunOnce() (<-chan *models.RunOnce, chan<- bool, <-chan error) {
	fakeBBS.CompletedRunOnceChan = make(chan *models.RunOnce)
	fakeBBS.CompletedRunOnceErrChan = make(chan error)
	fakeBBS.CalledCompletedRunOnce <- true
	return fakeBBS.CompletedRunOnceChan, nil, fakeBBS.CompletedRunOnceErrChan
}

func (fakeBBS *FakeStagerBBS) ResolvingRunOnce(runOnce *models.RunOnce) error {
	fakeBBS.ResolvingRunOnceInput.RunOnceToResolve = runOnce
	return fakeBBS.ResolvingRunOnceOutput.Err
}

func (fakeBBS *FakeStagerBBS) DesireRunOnce(runOnce *models.RunOnce) error {
	panic("implement me!")
}

func (fakeBBS *FakeStagerBBS) ResolveRunOnce(runOnce *models.RunOnce) error {
	fakeBBS.ResolvedRunOnce = runOnce
	return fakeBBS.ResolveRunOnceErr
}

func (fakeBBS *FakeStagerBBS) GetAvailableFileServer() (string, error) {
	panic("implement me!")
}
