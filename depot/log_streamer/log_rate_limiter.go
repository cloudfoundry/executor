package log_streamer

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	loggingclient "code.cloudfoundry.org/diego-logging-client"
	"golang.org/x/time/rate"
)

const (
	AppInstanceExceededLogRateLimitCount = "AppInstanceExceededLogRateLimitCount"
	LogRateLimitExceededLogInterval      = time.Second
)

// A logRateLimiter is used by the streamDestination to limit logs from an app instance.
// This can be done by limiting the number of lines per second,
// or by limiting the number of bytes per second.
type logRateLimiter struct {
	ctx          context.Context
	metronClient loggingclient.IngressClient

	maxLogLinesPerSecond         int
	maxLogBytesPerSecond         int64
	maxLogLinesPerSecondLimiter  *rate.Limiter
	maxLogBytesPerSecondLimiter  *rate.Limiter
	metricReportLimiter          *rate.Limiter
	logReportLimiter             *rate.Limiter
	logMetricsEmitInterval       time.Duration
	bytesEmittedLastInterval     uint64
	needToReportOverlimitMessage atomic.Value
}

func NewLogRateLimiter(
	ctx context.Context,
	metronClient loggingclient.IngressClient,
	maxLogLinesPerSecond int,
	maxLogBytesPerSecond int64,
	logMetricsEmitInterval time.Duration,
) *logRateLimiter {
	var needToReportOverlimitMessage atomic.Value
	needToReportOverlimitMessage.Store(true)
	limiter := &logRateLimiter{
		ctx:                          ctx,
		metronClient:                 metronClient,
		maxLogLinesPerSecond:         maxLogLinesPerSecond,
		maxLogBytesPerSecond:         maxLogBytesPerSecond,
		logMetricsEmitInterval:       logMetricsEmitInterval,
		bytesEmittedLastInterval:     0,
		needToReportOverlimitMessage: needToReportOverlimitMessage,
	}

	if maxLogLinesPerSecond > 0 {
		limiter.maxLogLinesPerSecondLimiter = rate.NewLimiter(rate.Limit(maxLogLinesPerSecond), maxLogLinesPerSecond)
	} else {
		limiter.maxLogLinesPerSecondLimiter = rate.NewLimiter(rate.Inf, 0)
	}
	if limiter.maxLogBytesPerSecond > -1 {
		limiter.maxLogBytesPerSecondLimiter = rate.NewLimiter(rate.Limit(maxLogBytesPerSecond), int(maxLogBytesPerSecond))
	} else {
		limiter.maxLogBytesPerSecondLimiter = rate.NewLimiter(rate.Inf, 0)
	}
	go limiter.emitMetrics()
	return limiter
}

// Limit is called before logging to determine if the log should be dropped (returns err) or logged (returns nil).
func (r *logRateLimiter) Limit(sourceName string, tags map[string]string, logLength int) error {
	if r.maxLogBytesPerSecond == 0 {
		return fmt.Errorf("Not allowed to log")
	}

	if !r.maxLogBytesPerSecondLimiter.AllowN(time.Now(), logLength) {
		reportMessage := fmt.Sprintf("app instance exceeded log rate limit (%d bytes/sec)", r.maxLogBytesPerSecond)
		r.reportOverlimit(sourceName, tags, reportMessage)
		return fmt.Errorf(reportMessage)
	}

	if !r.maxLogLinesPerSecondLimiter.Allow() {
		reportMessage := fmt.Sprintf("app instance exceeded log rate limit (%d log-lines/sec) set by platform operator", r.maxLogLinesPerSecond)
		r.reportOverlimit(sourceName, tags, reportMessage)
		return fmt.Errorf(reportMessage)
	}

	atomic.AddUint64(&r.bytesEmittedLastInterval, uint64(logLength))
	r.needToReportOverlimitMessage.Store(true)
	return nil
}

func (r *logRateLimiter) emitMetrics() {
	if r.logMetricsEmitInterval <= 0 {
		return
	}
	t := time.NewTicker(r.logMetricsEmitInterval)
	defer t.Stop()
	intervalDivider := r.logMetricsEmitInterval.Seconds()
	for {
		select {
		case <-t.C:
			lastIntervalEmitted := atomic.SwapUint64(&r.bytesEmittedLastInterval, 0)
			perSecondValue := float64(lastIntervalEmitted) / intervalDivider
			r.metronClient.SendBytesPerSecond("log_rate_limit", float64(r.maxLogBytesPerSecond))
			r.metronClient.SendBytesPerSecond("log_rate", perSecondValue)
		case <-r.ctx.Done():
			return
		}
	}
}

func (r *logRateLimiter) reportOverlimit(sourceName string, tags map[string]string, reportMessage string) {
	if r.needToReportOverlimitMessage.CompareAndSwap(true, false) {
		r.reportLogRateLimitExceededMetric()
		r.reportLogRateLimitExceededLog(sourceName, tags, reportMessage)
	}
}

func (r *logRateLimiter) reportLogRateLimitExceededMetric() {
	_ = r.metronClient.IncrementCounter(AppInstanceExceededLogRateLimitCount)
}

func (r *logRateLimiter) reportLogRateLimitExceededLog(sourceName string, tags map[string]string, reportMessage string) {
	_ = r.metronClient.SendAppLog(reportMessage, sourceName, tags)
}
