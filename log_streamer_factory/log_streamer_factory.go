package log_streamer_factory

import (
	"github.com/cloudfoundry-incubator/executor/actionrunner/logstreamer"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/loggregatorlib/emitter"
	"strconv"
)

type LogStreamerFactory func(models.LogConfig) logstreamer.LogStreamer

func New(loggregatorServer string, loggregatorSecret string) LogStreamerFactory {
	return func(logConfig models.LogConfig) logstreamer.LogStreamer {
		if logConfig.SourceName == "" {
			return nil
		}

		sourceId := ""
		if logConfig.Index != nil {
			sourceId = strconv.Itoa(*logConfig.Index)
		}

		logEmitter, _ := emitter.NewEmitter(
			loggregatorServer,
			logConfig.SourceName,
			sourceId,
			loggregatorSecret,
			nil,
		)

		return logstreamer.New(logConfig.Guid, logEmitter)
	}
}
