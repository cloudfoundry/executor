package containermetrics

import (
	"time"

	loggingclient "code.cloudfoundry.org/diego-logging-client"
	"code.cloudfoundry.org/executor"
	"code.cloudfoundry.org/lager"
)

type spikeInfo struct {
	start *time.Time
	end   *time.Time
}

type CPUSpikeHandler struct {
	spikeInfos   map[string]*spikeInfo
	metronClient loggingclient.IngressClient
}

func NewCPUSpikeHandler(metronClient loggingclient.IngressClient) *CPUSpikeHandler {
	return &CPUSpikeHandler{
		spikeInfos:   make(map[string]*spikeInfo),
		metronClient: metronClient,
	}
}

func (handler *CPUSpikeHandler) Handle(logger lager.Logger, container executor.Container, metric executor.Metrics, timeStamp time.Time) error {
	guid := container.Guid
	previousSpikeInfo := handler.spikeInfos[guid]
	currentSpikeInfo := &spikeInfo{}

	if previousSpikeInfo != nil {
		currentSpikeInfo.start = previousSpikeInfo.start
		currentSpikeInfo.end = previousSpikeInfo.end
	}

	if spikeStarted(metric, previousSpikeInfo) {
		currentSpikeInfo.start = &timeStamp
		handler.spikeInfos[guid] = currentSpikeInfo
		return nil
	}

	if spikeEnded(metric, previousSpikeInfo) {
		currentSpikeInfo.end = &timeStamp

		err := handler.metronClient.SendSpikeMetrics(loggingclient.SpikeMetric{
			Start: *currentSpikeInfo.start,
			End:   *currentSpikeInfo.end,
			Tags:  metric.MetricsConfig.Tags,
		})
		if err != nil {
			return err
		}

		handler.spikeInfos[guid] = nil
		return nil
	}

	return nil
}

func spikeStarted(metric executor.Metrics, previousSpikeInfo *spikeInfo) bool {
	currentlySpiking := uint64(metric.TimeSpentInCPU.Nanoseconds()) > metric.AbsoluteCPUEntitlementInNanoseconds
	previouslySpiking := previousSpikeInfo != nil && previousSpikeInfo.start != nil
	return currentlySpiking && !previouslySpiking
}

func spikeEnded(metric executor.Metrics, previousSpikeInfo *spikeInfo) bool {
	currentlySpiking := uint64(metric.TimeSpentInCPU.Nanoseconds()) > metric.AbsoluteCPUEntitlementInNanoseconds
	previouslySpiking := previousSpikeInfo != nil && previousSpikeInfo.start != nil
	return !currentlySpiking && previouslySpiking
}
