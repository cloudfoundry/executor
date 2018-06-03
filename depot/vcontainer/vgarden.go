package vcontainer

// maintain an adapter to the vcontainer  rpc service.
import (
	"context"

	"github.com/virtualcloudfoundry/vcontainercommon/verrors"

	"github.com/virtualcloudfoundry/vcontainercommon/vcontainermodels"

	"code.cloudfoundry.org/executor/model"
	"code.cloudfoundry.org/garden"
	"code.cloudfoundry.org/lager"
	google_protobuf "github.com/gogo/protobuf/types"
	vcmodels "github.com/virtualcloudfoundry/vcontainercommon/vcontainermodels"
)

type vgarden struct {
	config        vcmodels.VContainerClientConfig
	vgardenClient vcmodels.VGardenClient
	logger        lager.Logger
}

func NewVGardenWithAdapter(logger lager.Logger, config vcmodels.VContainerClientConfig) garden.Client {
	vgardenClient, err := NewVGardenClient(logger, config)
	if err != nil {
		logger.Error("new-vgarden-with-adapter", err)
	}
	return &vgarden{
		config:        config,
		vgardenClient: vgardenClient,
		logger:        logger,
	}
}

func (c *vgarden) Ping() error {
	c.logger.Info("vgarden-ping")
	_, err := c.vgardenClient.Ping(context.Background(), &google_protobuf.Empty{})
	if err != nil {
		c.logger.Error("vgarden-ping-failed", err)
		return err
	}
	return nil
}

func (c *vgarden) Capacity() (garden.Capacity, error) {
	c.logger.Info("vgarden-capacity")
	capacity, err := c.vgardenClient.Capacity(context.Background(), &google_protobuf.Empty{})
	if err != nil {
		c.logger.Error("vgarden-get-capacity-failed", err)
		return garden.Capacity{}, nil
	} else {
		c.logger.Info("vgarden-got-capacity", lager.Data{"capacity": capacity})
		return garden.Capacity{
			MemoryInBytes: capacity.MemoryInBytes,
			DiskInBytes:   capacity.DiskInBytes,
			MaxContainers: capacity.MaxContainers,
		}, nil
	}
}

func (c *vgarden) Create(spec garden.ContainerSpec) (garden.Container, error) {
	c.logger.Info("vgarden-create", lager.Data{"spec": spec})
	// if the limit is null set it a default value
	if spec.Limits.Memory.LimitInBytes == 0 {
		flo := (0.1 * 1024 * 1024 * 1024)
		spec.Limits.Memory.LimitInBytes = uint64(flo)
	}
	if spec.Limits.CPU.LimitInShares == 0 {
		spec.Limits.CPU.LimitInShares = 1
	}
	specRemote, _ := ConvertContainerSpec(spec)
	_, err := c.vgardenClient.Create(context.Background(), specRemote)
	if err != nil {
		c.logger.Error("vgarden-create-container-failed", err)
		return nil, verrors.New("vgarden-create-container-failed")
	}
	container := NewVContainer(spec.Handle, c.logger, model.GetExecutorEnvInstance().VContainerClientConfig)
	return container, nil
}

func (c *vgarden) Containers(properties garden.Properties) ([]garden.Container, error) {
	// we only support get all containers.
	c.logger.Info("vgarden-containers", lager.Data{"properties": properties})

	// convert the properties.
	containers, err := c.vgardenClient.Containers(context.Background(), &vcontainermodels.Properties{
		Properties: properties,
	})
	if err != nil {
		c.logger.Error("vgarden-containers", err)
		return nil, verrors.New("vgarden-containers-rpc-failed")
	}
	var gardenContainers []garden.Container
	if containers == nil || containers.Handle == nil {
		return gardenContainers, nil
	} else {
		gardenContainers = make([]garden.Container, len(containers.Handle))
		for i, handle := range containers.Handle {
			container := NewVContainer(handle, c.logger, model.GetExecutorEnvInstance().VContainerClientConfig)
			gardenContainers[i] = container
		}
	}
	return gardenContainers, nil
}

func (c *vgarden) Destroy(handle string) error {
	c.logger.Info("vgarden-destroy", lager.Data{"handle": handle})

	_, err := c.vgardenClient.Destroy(context.Background(), &google_protobuf.StringValue{
		Value: handle,
	})
	if err != nil {
		c.logger.Error("destroy", err)
		return err
	} else {
		return nil
	}
}

func (c *vgarden) BulkInfo(handles []string) (map[string]garden.ContainerInfoEntry, error) {
	c.logger.Info("vgarden-bulkinfo")
	_, err := c.vgardenClient.BulkInfo(context.Background(), &vcontainermodels.BulkInfoRequest{
		Handles: handles,
	})
	if err != nil {
		c.logger.Error("vgarden-bulkinfo", err)
		return nil, err
	} else {
		return nil, verrors.New("vgarden-bulk-info-not-implemented")
	}
}

func (c *vgarden) BulkMetrics(handles []string) (map[string]garden.ContainerMetricsEntry, error) {
	c.logger.Info("vgarden-bulkmetrics")
	bulkMetrics, err := c.vgardenClient.BulkMetrics(context.Background(), &vcontainermodels.BulkMetricsRequest{
		Handles: handles,
	})
	if err != nil {
		c.logger.Error("vgarden-bulkmetrics", err)
		return nil, verrors.New("vgarden-bulk-metrics-failed")
	} else {
		// TODO return the real data.
		c.logger.Info("vgarden-bulkmetrics", lager.Data{"metrics": bulkMetrics})
		return nil, nil
	}
}

func (c *vgarden) Lookup(handle string) (garden.Container, error) {
	c.logger.Info("vgarden-lookup")
	_, err := c.vgardenClient.Lookup(context.Background(), &google_protobuf.StringValue{
		Value: handle,
	})
	if err != nil {
		c.logger.Error("vgarden-lookup-failed", err)
		return nil, err
	}
	return nil, verrors.New("vgarden-lookup-not-implemented")
}
