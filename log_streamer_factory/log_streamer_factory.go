package log_streamer_factory

import (
	"github.com/cloudfoundry-incubator/executor/actionrunner/logstreamer"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/loggregatorlib/emitter"
	"strconv"
)

type LogStreamerFactory struct {
	loggregatorServer string
	loggregatorSecret string
}

func New(
	loggregatorServer string,
	loggregatorSecret string,
) *LogStreamerFactory {
	return &LogStreamerFactory{
		loggregatorServer: loggregatorServer,
		loggregatorSecret: loggregatorSecret,
	}
}

func (factory *LogStreamerFactory) Make(logConfig models.LogConfig) logstreamer.LogStreamer {
	sourceId := ""
	if logConfig.Index != nil {
		sourceId = strconv.Itoa(*logConfig.Index)
	}

	logEmitter, _ := emitter.NewEmitter(
		factory.loggregatorServer,
		logConfig.SourceName,
		sourceId,
		factory.loggregatorSecret,
		nil,
	)

	return logstreamer.New(logConfig.Guid, logEmitter)
}
