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
	LogRateLimitAllowDelta               = time.Millisecond
)

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
		limiter.maxLogLinesPerSecondLimiter = rate.NewLimiter(rate.Every(time.Second/time.Duration(maxLogLinesPerSecond)), maxLogLinesPerSecond)
	} else {
		limiter.maxLogLinesPerSecondLimiter = rate.NewLimiter(rate.Inf, 0)
	}
	// TODO: should this be -1
	if limiter.maxLogBytesPerSecond > 0 {
		limiter.maxLogBytesPerSecondLimiter = rate.NewLimiter(rate.Every(time.Second/time.Duration(maxLogBytesPerSecond)), int(maxLogBytesPerSecond))
	} else {
		limiter.maxLogBytesPerSecondLimiter = rate.NewLimiter(rate.Inf, 0)
	}

	return limiter
}

func (r *logRateLimiter) Limit(sourceName string, tags map[string]string, logLength int) error {
	if r.maxLogBytesPerSecond == 0 {
		return fmt.Errorf("Not allowed to log")
	}
	now := time.Now()

	reservationBytes := r.maxLogBytesPerSecondLimiter.ReserveN(now, logLength)
	if !reservationBytes.OK() {
		return fmt.Errorf("bytes would exceed burst capacity")
	}
	//Reservation is for 1, so failure to get reservation is impossible
	reservationLines := r.maxLogLinesPerSecondLimiter.ReserveN(now, 1)

	delayLines := reservationLines.DelayFrom(now)
	delayBytes := reservationBytes.DelayFrom(now)
	if delayLines < LogRateLimitAllowDelta && delayBytes < LogRateLimitAllowDelta {
		// log immediately
		return nil
	}
	var delay time.Duration
	var reportMessage string
	if delayBytes > delayLines {
		delay = delayBytes
		reportMessage = fmt.Sprintf("app instance exceeded log rate limit (%d bytes/sec)", r.maxLogBytesPerSecond)
	} else {
		delay = delayLines
		reportMessage = fmt.Sprintf("app instance exceeded log rate limit (%d log-lines/sec) set by platform operator", r.maxLogLinesPerSecond)
	}
	err := r.overLimit(sourceName, tags, delay, reportMessage)
	return err
}

func (r *logRateLimiter) overLimit(sourceName string, tags map[string]string, delay time.Duration, reportMessage string) error {
	t := time.NewTimer(delay)
	defer t.Stop()
	select {
	case <-t.C:
		break
	case <-r.ctx.Done():
		return r.ctx.Err()
	}

	r.reportLogRateLimitExceededMetric()
	r.reportLogRateLimitExceededLog(sourceName, tags, reportMessage)
	return fmt.Errorf("Not allowed to log")
}

func (r *logRateLimiter) reportLogRateLimitExceededMetric() {
	now := time.Now()
	//ReservN cannot fail here from 1 sized reservation infinite deadline
	metricReporterReservation := r.metricReportLimiter.ReserveN(now, 1)
	metricReportDelay := metricReporterReservation.DelayFrom(now)
	if metricReportDelay < LogRateLimitAllowDelta {
		r.metronClient.IncrementCounter(AppInstanceExceededLogRateLimitCount)
	} else {
		metricReporterReservation.CancelAt(now)
	}
}
func (r *logRateLimiter) reportLogRateLimitExceededLog(sourceName string, tags map[string]string, reportMessage string) {
	now := time.Now()
	//ReservN cannot fail here from 1 sized reservation infinite deadline
	logReporterReservation := r.logReportLimiter.ReserveN(now, 1)
	logReportDelay := logReporterReservation.DelayFrom(now)
	if logReportDelay < LogRateLimitAllowDelta {
		r.metronClient.SendAppLog(string(reportMessage), sourceName, tags)
	} else {
		logReporterReservation.CancelAt(now)
	}
}
