package steps

import (
	"time"

	"code.cloudfoundry.org/clock"
	"code.cloudfoundry.org/executor/depot/log_streamer"
	"code.cloudfoundry.org/lager"
	"code.cloudfoundry.org/workpool"
	"github.com/tedsuo/ifrit"
)

func NewMonitor(
	checkFunc func() Step,
	hasStartedRunning chan<- struct{},
	logger lager.Logger,
	clock clock.Clock,
	logStreamer log_streamer.LogStreamer,
	startTimeout time.Duration,
	healthyInterval time.Duration,
	unhealthyInterval time.Duration,
	workPool *workpool.WorkPool,
	proxyReadinessChecks ...Step,
) Step {
	throttledCheckFunc := func() ifrit.Runner {
		return NewThrottle(checkFunc(), workPool)
	}

	//	readiness := NewEventuallySucceedsStep(throttledCheckFunc, unhealthyInterval, startTimeout, clock)
	//	liveness := NewConsistentlySucceedsStep(throttledCheckFunc, healthyInterval, clock)
	//
	//	// add the proxy readiness checks (if any)
	//	readiness = NewParallel(append(proxyReadinessChecks, readiness))
	//
	//	return NewHealthCheckStep(readiness, liveness, hasStartedRunning, logger, clock, logStreamer, logStreamer, startTimeout)

	return nil
}
