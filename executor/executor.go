package executor

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"syscall"
	"time"

	"github.com/cloudfoundry-incubator/garden/warden"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/nu7hatch/gouuid"

	"github.com/cloudfoundry-incubator/executor/api"
	"github.com/cloudfoundry-incubator/executor/registry"
	"github.com/cloudfoundry-incubator/executor/transformer"
)

const ServerCloseErrMsg = "use of closed network connection"

type Executor struct {
	id                    string
	apiURL                string
	containerMaxCPUShares uint64
	registry              registry.Registry
	wardenClient          warden.Client
	transformer           *transformer.Transformer
	drainTimeout          time.Duration
	logger                *steno.Logger
	listener              net.Listener
	serverErrChan         chan error
}

var ErrDrainTimeout = errors.New("tasks did not complete within timeout")

func New(
	apiURL string,
	containerMaxCPUShares uint64,
	maxMemoryMB int,
	maxDiskMB int,
	wardenClient warden.Client,
	transformer *transformer.Transformer,
	drainTimeout time.Duration,
	logger *steno.Logger,
) *Executor {

	uuid, err := uuid.NewV4()
	if err != nil {
		panic("Failed to generate a random guid....:" + err.Error())
	}

	executorID := uuid.String()
	capacity := registry.Capacity{MemoryMB: maxMemoryMB, DiskMB: maxDiskMB}
	reg := registry.New(executorID, capacity)

	return &Executor{
		id:                    executorID,
		apiURL:                apiURL,
		containerMaxCPUShares: containerMaxCPUShares,
		registry:              reg,
		wardenClient:          wardenClient,
		transformer:           transformer,
		drainTimeout:          drainTimeout,
		logger:                logger,
		serverErrChan:         make(chan error),
	}
}

func (e *Executor) ID() string {
	return e.id
}

func (e *Executor) startServer(router http.Handler) (err error) {
	e.listener, err = net.Listen("tcp", e.apiURL)
	if err != nil {
		return
	}
	go func() {
		e.serverErrChan <- http.Serve(e.listener, router)
	}()
	return
}

func (e *Executor) stopServer() {
	if e.listener != nil {
		e.listener.Close()
		e.listener = nil
	}
}

func (e *Executor) Run(sigChan chan os.Signal, readyChan chan struct{}) error {
	router, err := api.New(&api.Config{
		Registry:              e.registry,
		WardenClient:          e.wardenClient,
		ContainerOwnerName:    fmt.Sprintf("executor-%s", e.ID()),
		ContainerMaxCPUShares: e.containerMaxCPUShares,
		Transformer:           e.transformer,
		Logger:                e.logger,
	})
	if err != nil {
		return err
	}

	err = e.startServer(router)
	if err != nil {
		return err
	}

	if readyChan != nil {
		close(readyChan)
	}

	for {
		select {
		case err := <-e.serverErrChan:
			if err != nil && err.Error() != ServerCloseErrMsg {
				e.logger.Errord(map[string]interface{}{
					"error": err.Error(),
				}, "executor.server.failed")
			}
			return err

		case signal := <-sigChan:
			switch signal {
			case syscall.SIGINT, syscall.SIGTERM:
				e.logger.Info("executor.stoping")
				e.stopServer()
				e.logger.Info("executor.stopped")
				return nil

			case syscall.SIGUSR1:
				e.logger.Info("executor.draining")
				e.stopServer()
				e.logger.Info("executor.drained")
				return nil
			}
		}
	}
}
