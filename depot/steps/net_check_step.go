package steps

import (
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/cloudfoundry-incubator/bbs/models"
	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/executor/depot/log_streamer"
	"github.com/pivotal-golang/lager"
)

type netCheckStep struct {
	model      models.NetCheckAction
	streamer   log_streamer.LogStreamer
	logger     lager.Logger
	externalIP string
	ports      []executor.PortMapping

	*canceller
}

func NewNetCheck(
	model models.NetCheckAction,
	streamer log_streamer.LogStreamer,
	logger lager.Logger,
	externalIP string,
	ports []executor.PortMapping,
) *netCheckStep {
	logger = logger.Session("run-step")
	return &netCheckStep{
		model:      model,
		streamer:   streamer,
		logger:     logger,
		externalIP: externalIP,
		ports:      ports,

		canceller: newCanceller(),
	}
}

func (step *netCheckStep) Perform() error {
	step.logger.Info("running")

	cancel := step.Cancelled()

	errChan := make(chan error, 1)

	go func() {
		var port uint16
		for _, portMapping := range step.ports {
			if uint32(portMapping.ContainerPort) == step.model.Port {
				port = portMapping.HostPort
				break
			}
		}
		addr := fmt.Sprintf("%s:%d", step.externalIP, port)
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			conn.Close()
			fmt.Fprintln(step.streamer.Stdout(), "net check healthcheck passed")
		}
		errChan <- err
	}()

	select {
	case err := <-errChan:
		if err != nil {
			step.logger.Error("net-check-error", err)
		}
		return err

	case <-cancel:
		step.logger.Debug("signalling-terminate")
		return errors.New("cancelling-net-check")

	}

	panic("unreachable")
}
