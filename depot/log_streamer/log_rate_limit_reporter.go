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

type logRateLimitReporter struct {
	ctx          context.Context
	metronClient loggingclient.IngressClient

	maxLogLinesPerSecond        int
	maxLogLinesPerSecondLimiter *rate.Limiter
	metricReportLimiter         *rate.Limiter
	logReportLimiter            *rate.Limiter
}

func newLogRateLimitReporter(
	ctx context.Context,
	metronClient loggingclient.IngressClient,
	maxLogLinesPerSecond int,
	logRateLimitExceededReportInterval time.Duration,
) *logRateLimitReporter {
	reporter := &logRateLimitReporter{
		ctx:                  ctx,
		metronClient:         metronClient,
		maxLogLinesPerSecond: maxLogLinesPerSecond,
		metricReportLimiter:  rate.NewLimiter(rate.Every(logRateLimitExceededReportInterval), 1),
		logReportLimiter:     rate.NewLimiter(rate.Every(LogRateLimitExceededLogInterval), 1),
	}

	if maxLogLinesPerSecond > 0 {
		reporter.maxLogLinesPerSecondLimiter = rate.NewLimiter(rate.Every(time.Second/time.Duration(maxLogLinesPerSecond)), maxLogLinesPerSecond)
	}

	return reporter
}

func (r *logRateLimitReporter) Report(sourceName string, tags map[string]string) error {
	if r.maxLogLinesPerSecond == 0 {
		return nil
	}

	now := time.Now()

	reservation := r.maxLogLinesPerSecondLimiter.ReserveN(now, 1)
	if !reservation.OK() {
		return fmt.Errorf("rate would exceed context deadline")
	}

	delay := reservation.DelayFrom(now)
	if delay < LogRateLimitAllowDelta {
		return nil
	}

	t := time.NewTimer(delay)
	defer t.Stop()
	select {
	case <-t.C:
		break
	case <-r.ctx.Done():
		reservation.Cancel()
		return r.ctx.Err()
	}

	now = time.Now()
	metricReporterReservation := r.metricReportLimiter.ReserveN(now, 1)
	if !metricReporterReservation.OK() {
		return fmt.Errorf("rate would exceed context deadline")
	}
	metricReportDelay := metricReporterReservation.DelayFrom(now)
	if metricReportDelay < LogRateLimitAllowDelta {
		r.metronClient.IncrementCounter(AppInstanceExceededLogRateLimitCount)
	} else {
		metricReporterReservation.CancelAt(now)
	}

	now = time.Now()
	logReporterReservation := r.logReportLimiter.ReserveN(now, 1)
	if !logReporterReservation.OK() {
		return fmt.Errorf("rate would exceed context deadline")
	}
	logReportDelay := logReporterReservation.DelayFrom(now)
	if logReportDelay < LogRateLimitAllowDelta {
		msg := fmt.Sprintf("app instance exceeded log rate limit (%d log-lines/sec) set by platform operator", r.maxLogLinesPerSecond)
		r.metronClient.SendAppLog(string(msg), sourceName, tags)
	} else {
		logReporterReservation.CancelAt(now)
	}

	return nil
}
