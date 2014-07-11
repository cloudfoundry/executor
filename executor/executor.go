package executor

import (
	"errors"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/cloudfoundry-incubator/garden/warden"
	steno "github.com/cloudfoundry/gosteno"

	"github.com/cloudfoundry-incubator/executor/registry"
	"github.com/cloudfoundry-incubator/executor/server"
	"github.com/cloudfoundry-incubator/executor/transformer"
)

const ServerCloseErrMsg = "use of closed network connection"

type Executor struct {
	apiURL                string
	containerOwnerName    string
	containerMaxCPUShares uint64
	registry              registry.Registry
	wardenClient          warden.Client
	transformer           *transformer.Transformer
	drainTimeout          time.Duration
	logger                *steno.Logger
	listener              net.Listener
	waitGroup             *sync.WaitGroup
	cancelChan            chan struct{}
	stoppedChan           chan error
	serverErrChan         chan error
}

var ErrDrainTimeout = errors.New("tasks did not complete within timeout")

func New(
	apiURL string,
	containerOwnerName string,
	containerMaxCPUShares uint64,
	registry registry.Registry,
	wardenClient warden.Client,
	transformer *transformer.Transformer,
	drainTimeout time.Duration,
	logger *steno.Logger,
) *Executor {
	return &Executor{
		apiURL:                apiURL,
		containerOwnerName:    containerOwnerName,
		containerMaxCPUShares: containerMaxCPUShares,
		registry:              registry,
		wardenClient:          wardenClient,
		transformer:           transformer,
		drainTimeout:          drainTimeout,
		logger:                logger,
		waitGroup:             &sync.WaitGroup{},
		cancelChan:            make(chan struct{}),
		stoppedChan:           make(chan error, 1),
		serverErrChan:         make(chan error),
	}
}

func (e *Executor) Run(sigChan <-chan os.Signal, readyChan chan<- struct{}) error {
	err := e.init()
	if err != nil {
		return err
	}

	close(readyChan)

	stopping := false

	for {
		select {
		case err := <-e.serverErrChan:
			if err != nil && !strings.Contains(err.Error(), ServerCloseErrMsg) {
				e.logger.Errord(map[string]interface{}{
					"error": err.Error(),
				}, "executor.server.failed")
				return err
			}
			e.logger.Info("executor.server.stopped")

		case signal := <-sigChan:
			if stopping {
				e.logger.Info("executor.signal.ignored")
				break
			}

			switch signal {
			case syscall.SIGINT, syscall.SIGTERM:
				e.logger.Info("executor.stopping")
				stopping = true
				e.drain(0)
			case syscall.SIGUSR1:
				e.logger.Info("executor.draining")
				stopping = true
				e.drain(e.drainTimeout)
			}

		case err := <-e.stoppedChan:
			e.logger.Info("executor.stopped")
			return err
		}
	}
}

func (e *Executor) init() error {
	err := e.destroyContainers()
	if err != nil {
		return err
	}

	router, err := server.New(&server.Config{
		Registry:              e.registry,
		WardenClient:          e.wardenClient,
		ContainerOwnerName:    e.containerOwnerName,
		ContainerMaxCPUShares: e.containerMaxCPUShares,
		Transformer:           e.transformer,
		WaitGroup:             e.waitGroup,
		Cancel:                e.cancelChan,
		Logger:                e.logger,
	})
	if err != nil {
		return err
	}

	return e.startServer(router)
}

func (e *Executor) drain(drainTimeout time.Duration) {
	e.stopServer()

	time.AfterFunc(drainTimeout, func() {
		close(e.cancelChan)

		// This whole thing should be done better.
		// Stop-gap solution so deploys don't hang.
		// See: https://www.pivotaltracker.com/story/show/73640912
		time.Sleep(10 * time.Second)
		e.logger.Fatal("executor.shutting-down.failed-to-drain.shutting-down-violently")
	})

	go func() {
		e.logger.Info("executor.shutting-down.waiting-on-requests")
		e.waitGroup.Wait()
		e.logger.Info("executor.shutting-down.done-waiting-on-requests")
		e.stoppedChan <- nil
	}()
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

func (e *Executor) destroyContainers() error {
	containers, err := e.wardenClient.Containers(warden.Properties{
		"owner": e.containerOwnerName,
	})
	if err != nil {
		return err
	}

	for _, container := range containers {
		e.logger.Infod(
			map[string]interface{}{
				"handle": container.Handle(),
			},
			"executor.destroy-container",
		)
		err := e.wardenClient.Destroy(container.Handle())
		if err != nil {
			return err
		}
	}

	return nil
}
