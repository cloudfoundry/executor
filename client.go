package executor

import (
	"io"

	"github.com/pivotal-golang/lager"
)

//go:generate counterfeiter -o fakes/fake_client.go . Client

type Client interface {
	Ping() error
	AllocateContainers(requests []AllocationRequest) (map[string]string, error)
	GetContainer(guid string) (Container, error)
	RunContainer(*RunRequest) error
	StopContainer(guid string) error
	DeleteContainer(guid string) error
	ListContainers(Tags) ([]Container, error)
	GetAllMetrics(Tags) (map[string]Metrics, error)
	GetMetrics(guid string) (ContainerMetrics, error)
	RemainingResources() (ExecutorResources, error)
	TotalResources() (ExecutorResources, error)
	GetFiles(guid string, path string) (io.ReadCloser, error)
	SubscribeToEvents() (EventSource, error)
	Cleanup()
}

type ClientProvider interface {
	WithLogger(logger lager.Logger) Client
}

type WorkPoolSettings struct {
	CreateWorkPoolSize  int
	DeleteWorkPoolSize  int
	ReadWorkPoolSize    int
	MetricsWorkPoolSize int
}

//go:generate counterfeiter -o fakes/fake_event_source.go . EventSource

type EventSource interface {
	Next() (Event, error)
	Close() error
}

type AllocationRequest struct {
	Guid string
	Resource
	Tags
}

func NewAllocationRequest(guid string, resource *Resource, tags Tags) AllocationRequest {
	return AllocationRequest{
		Guid:     guid,
		Resource: *resource,
		Tags:     tags,
	}
}

func (a *AllocationRequest) Validate() error {
	if a.Guid == "" {
		return ErrGuidNotSpecified
	}
	return nil
}

type RunRequest struct {
	Guid string
	RunInfo
	Tags
}

func NewRunRequest(guid string, runInfo *RunInfo, tags Tags) RunRequest {
	return RunRequest{
		Guid:    guid,
		RunInfo: *runInfo,
		Tags:    tags,
	}
}
