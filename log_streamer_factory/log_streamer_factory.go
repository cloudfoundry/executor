package log_streamer_factory

import (
	"github.com/cloudfoundry-incubator/executor/log_streamer"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/loggregatorlib/emitter"
	"strconv"
)

type LogStreamerFactory func(models.LogConfig) log_streamer.LogStreamer

func New(loggregatorServer string, loggregatorSecret string) LogStreamerFactory {
	return func(logConfig models.LogConfig) log_streamer.LogStreamer {
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

		return log_streamer.New(logConfig.Guid, logEmitter)
	}
}
