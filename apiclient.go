package executor

import (
	"io"

	"github.com/pivotal-golang/lager"
)

//go:generate counterfeiter -o fakes/fake_client.go . Client

type Client interface {
	Ping() error
	CreateVolume(sizeMB int) (volumeID string, err error)
	AllocateContainers(requests []Container) (map[string]string, error)
	GetContainer(guid string) (Container, error)
	RunContainer(guid string) error
	StopContainer(guid string) error
	DeleteContainer(guid string) error
	ListContainers(Tags) ([]Container, error)
	RemainingResources() (ExecutorResources, error)
	TotalResources() (ExecutorResources, error)
	GetFiles(guid string, path string) (io.ReadCloser, error)
	SubscribeToEvents() (EventSource, error)
}

type ClientProvider interface {
	WithLogger(logger lager.Logger) Client
}

//go:generate counterfeiter -o fakes/fake_event_source.go . EventSource

type EventSource interface {
	Next() (Event, error)
	Close() error
}
