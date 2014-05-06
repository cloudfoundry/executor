package maintain

import (
	"errors"
	"os"
	"syscall"
	"time"

	Bbs "github.com/cloudfoundry-incubator/runtime-schema/bbs"
	steno "github.com/cloudfoundry/gosteno"
)

type Maintainer struct {
	id                string
	bbs               Bbs.ExecutorBBS
	logger            *steno.Logger
	heartbeatInterval time.Duration
}

func New(id string, bbs Bbs.ExecutorBBS, logger *steno.Logger, heartbeatInterval time.Duration) *Maintainer {
	return &Maintainer{
		id:                id,
		bbs:               bbs,
		logger:            logger,
		heartbeatInterval: heartbeatInterval,
	}
}

func (m *Maintainer) Run(sigChan chan os.Signal, ready chan struct{}) error {
	presence, status, err := m.bbs.MaintainExecutorPresence(m.heartbeatInterval, m.id)
	if err != nil {
		m.logger.Errord(map[string]interface{}{
			"error": err.Error(),
		}, "executor.maintain_presence_begin.failed")
	}

	if ready != nil {
		close(ready)
	}

	for {
		select {
		case sig := <-sigChan:
			switch sig {
			case syscall.SIGINT, syscall.SIGTERM:
				presence.Remove()
				return nil
			}

		case locked, ok := <-status:
			if !ok {
				return nil
			}

			if !locked {
				m.logger.Error("executor.maintain_presence.failed")
				return errors.New("Failed to maintain presence")
			}
		}
	}
}
