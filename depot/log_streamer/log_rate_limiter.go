package log_streamer

import (
	"context"
	"fmt"
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

	maxLogLinesPerSecond        int
	maxLogBytesPerSecond        int64
	maxLogLinesPerSecondLimiter *rate.Limiter
	maxLogBytesPerSecondLimiter *rate.Limiter
	metricReportLimiter         *rate.Limiter
	logReportLimiter            *rate.Limiter
}

func newLogRateLimiter(
	ctx context.Context,
	metronClient loggingclient.IngressClient,
	maxLogLinesPerSecond int,
	maxLogBytesPerSecond int64,
	logRateLimitExceededReportInterval time.Duration,
) *logRateLimiter {
	limiter := &logRateLimiter{
		ctx:                  ctx,
		metronClient:         metronClient,
		maxLogLinesPerSecond: maxLogLinesPerSecond,
		maxLogBytesPerSecond: maxLogBytesPerSecond,
		metricReportLimiter:  rate.NewLimiter(rate.Every(logRateLimitExceededReportInterval), 1),
		logReportLimiter:     rate.NewLimiter(rate.Every(LogRateLimitExceededLogInterval), 1),
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

	return nil
}

func (r *logRateLimiter) reportOverlimit(sourceName string, tags map[string]string, reportMessage string) {
	r.reportLogRateLimitExceededMetric()
	r.reportLogRateLimitExceededLog(sourceName, tags, reportMessage)
}

func (r *logRateLimiter) reportLogRateLimitExceededMetric() {
	if r.metricReportLimiter.Allow() {
		_ = r.metronClient.IncrementCounter(AppInstanceExceededLogRateLimitCount)
	}
}

func (r *logRateLimiter) reportLogRateLimitExceededLog(sourceName string, tags map[string]string, reportMessage string) {
	if r.logReportLimiter.Allow() {
		_ = r.metronClient.SendAppLog(reportMessage, sourceName, tags)
	}
}
