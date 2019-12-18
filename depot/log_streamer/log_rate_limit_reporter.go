package log_streamer

import (
	"context"
	"fmt"
	"sync"
	"time"

	loggingclient "code.cloudfoundry.org/diego-logging-client"
	"golang.org/x/time/rate"
)

const AppInstanceExceededLogRateLimitCount = "AppInstanceExceededLogRateLimitCount"

type logRateLimitReporter struct {
	ctx          context.Context
	metronClient loggingclient.IngressClient

	maxLogLinesPerSecond               int
	maxLogLinesPerSecondLimiter        *rate.Limiter
	logRateLimitExceededReportInterval time.Duration
	logRateLimitReportTime             time.Time
	logRateLimitReportTimeMux          *sync.Mutex
}

func newLogRateLimitReporter(
	ctx context.Context,
	metronClient loggingclient.IngressClient,
	maxLogLinesPerSecond int,
	logRateLimitExceededReportInterval time.Duration,
) *logRateLimitReporter {
	perSec := rate.Every(time.Second)
	lim := rate.NewLimiter(perSec, maxLogLinesPerSecond)

	return &logRateLimitReporter{
		ctx:                                ctx,
		metronClient:                       metronClient,
		maxLogLinesPerSecond:               maxLogLinesPerSecond,
		maxLogLinesPerSecondLimiter:        lim,
		logRateLimitExceededReportInterval: logRateLimitExceededReportInterval,
		logRateLimitReportTimeMux:          &sync.Mutex{},
	}
}

func (r *logRateLimitReporter) Report() error {
	if r.maxLogLinesPerSecond == 0 {
		return nil
	}

	reservation := r.maxLogLinesPerSecondLimiter.Reserve()
	if !reservation.OK() {
		return fmt.Errorf("rate would exceed context deadline")
	}

	now := time.Now()
	delay := reservation.DelayFrom(now)
	if delay == 0 {
		return nil
	}

	r.logRateLimitReportTimeMux.Lock()
	if r.logRateLimitReportTime.Add(r.logRateLimitExceededReportInterval).Before(now) {
		r.metronClient.IncrementCounter(AppInstanceExceededLogRateLimitCount)
		r.logRateLimitReportTime = now
	}
	r.logRateLimitReportTimeMux.Unlock()

	t := time.NewTimer(delay)
	defer t.Stop()
	select {
	case <-t.C:
		break
	case <-r.ctx.Done():
		reservation.Cancel()
		return r.ctx.Err()
	}

	return nil
}
