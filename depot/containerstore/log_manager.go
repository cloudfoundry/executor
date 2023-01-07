package containerstore

import (
	"time"

	loggingclient "code.cloudfoundry.org/diego-logging-client"
	"code.cloudfoundry.org/executor"
	"code.cloudfoundry.org/executor/depot/log_streamer"
)

//go:generate counterfeiter -o containerstorefakes/fake_log_manager.go . LogManager
type LogManager interface {
	NewLogStreamer(conf executor.LogConfig, metronClient loggingclient.IngressClient, maxLogLinesPerSecond int, maxLogBytesPerSecond int64, metricReportInterval time.Duration) log_streamer.LogStreamer
}

type logManager struct{}

func NewLogManager() LogManager {
	return &logManager{}
}

func (l *logManager) NewLogStreamer(conf executor.LogConfig, metronClient loggingclient.IngressClient, maxLogLinesPerSecond int, maxLogBytesPerSecond int64, metricReportInterval time.Duration) log_streamer.LogStreamer {
	return log_streamer.New(
		conf,
		metronClient,
		maxLogLinesPerSecond,
		maxLogBytesPerSecond,
		metricReportInterval,
	)
}
