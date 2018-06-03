package vcontainer

// maintain an adapter to the vcontainer rpc service.

import (
	"io"
	"time"

	"code.cloudfoundry.org/garden"
	"code.cloudfoundry.org/lager"
	google_protobuf "github.com/gogo/protobuf/types"
	"github.com/virtualcloudfoundry/vcontainercommon"
	"github.com/virtualcloudfoundry/vcontainercommon/vcontainermodels"
	"github.com/virtualcloudfoundry/vcontainercommon/verrors"
	context "golang.org/x/net/context"
	"google.golang.org/grpc/metadata"
)

type VContainer struct {
	handle           string
	vcontainerClient vcontainermodels.VContainerClient
	vprocessClient   vcontainermodels.VProcessClient
	logger           lager.Logger
}

func NewVContainer(handle string, logger lager.Logger, config vcontainermodels.VContainerClientConfig) garden.Container {
	vcontainerClient, err := NewVContainerClient(logger, config)
	if err != nil {
		logger.Error("new-vcontainer-client-failed", err)
	}
	vprocessClient, err := NewVProcessClient(logger, config)
	if err != nil {
		logger.Error("new-vprocess-client-failed", err)
	}
	return &VContainer{
		handle:           handle,
		vcontainerClient: vcontainerClient,
		vprocessClient:   vprocessClient,
		logger:           logger,
	}
}

func (c *VContainer) Handle() string {
	return c.handle
}

func (c *VContainer) Stop(kill bool) error {
	c.logger.Info("vcontainer-stop")

	ctx := c.buildContext()
	_, err := c.vcontainerClient.Stop(ctx, &vcontainermodels.StopMessage{
		Kill: kill,
	})

	if err != nil {
		c.logger.Error("vcontainer-stop", err, lager.Data{"kill": kill})
	}
	return err
}

func (c *VContainer) Info() (garden.ContainerInfo, error) {
	c.logger.Info("vcontainer-info")
	ctx := c.buildContext()
	newInfo, err := c.vcontainerClient.Info(ctx, &google_protobuf.Empty{})
	if err != nil {
		c.logger.Error("vcontainer-get-info-failed", err, lager.Data{"handle": c.handle})
		return garden.ContainerInfo{}, verrors.New("vcontainer-info-failed")
	} else {
		c.logger.Info("vcontainer-info-vc-new-info", lager.Data{"info": newInfo})
		return ConvertContainerInfo(*newInfo), err
	}
}

func (c *VContainer) StreamIn(spec garden.StreamInSpec) error {
	c.logger.Info("vcontainer-stream-in")
	// TODO move share creation logic to here.
	ctx := c.buildContext()
	client, err := c.vcontainerClient.StreamIn(ctx)
	if err != nil {
		c.logger.Error("vcontainer-stream-in-open-rpc-failed", err)
		return verrors.New("vcontainer-stream-in-open-rpc-failed")
	} else {
		message := &vcontainermodels.StreamInSpec{
			Part: &vcontainermodels.StreamInSpec_Path{
				Path: spec.Path,
			},
		}

		err := client.Send(message)
		if err != nil {
			c.logger.Error("vcontainer-stream-in-spec-send-path-failed", err)
			return verrors.New("vcontainer-stream-in-send-msg-failed")
		}

		message = &vcontainermodels.StreamInSpec{
			Part: &vcontainermodels.StreamInSpec_User{
				User: spec.User,
			},
		}

		err = client.Send(message)
		if err != nil {
			c.logger.Error("vcontainer-stream-in-spec-send-user-failed", err)
			return verrors.New("vcontainer-stream-in-send-msg-failed")
		} else {

			// send the content now
			buf := make([]byte, 32*1024)
			bytesSent := 0
			for {
				nr, er := spec.TarStream.Read(buf)
				if er != nil {
					if er != io.EOF {
						err = er
					}
					break
				} else {
					if nr > 0 {
						bytesSent += nr
						message = &vcontainermodels.StreamInSpec{
							Part: &vcontainermodels.StreamInSpec_Content{
								Content: buf[:nr],
							},
						}
						err = client.Send(message)
						if err != nil {
							break
						}
					}
				}
			}
			if err != nil {
				c.logger.Error("vcontainer-stream-in-spec-send-content-failed", err)
				return verrors.New("vcontainer-stream-in-send-msg-failed")
			} else {
				streamInResponse, err := client.CloseAndRecv()
				if err != nil {
					c.logger.Error("vcontainer-stream-in-spec-close-and-recv-failed", err)
					return verrors.New("vcontainer-stream-in-close-and-recv-failed")
				} else {
					if streamInResponse != nil {
						c.logger.Info("vcontainer-stream-in-spec-close-and-recv", lager.Data{"message": streamInResponse.Message})
					}
				}
			}
			c.logger.Info("vcontainer-stream-in-bytes-sent", lager.Data{"bytes": bytesSent})
		}
	}
	return nil
}

func (c *VContainer) StreamOut(spec garden.StreamOutSpec) (io.ReadCloser, error) {
	c.logger.Info("vcontainer-stream-out")
	ctx := c.buildContext()

	client, err := c.vcontainerClient.StreamOut(ctx, &vcontainermodels.StreamOutSpec{
		Path: spec.Path,
		User: spec.User,
	})

	if err != nil {
		c.logger.Error("vcontainer-stream-out-stream-out-failed", err)
		return nil, verrors.New("vcontainer open stream out rpc failed.")
	} else {
		stream := NewStreamOutAdapter(c.logger, client)
		return stream, nil
	}
}

func (c *VContainer) CurrentBandwidthLimits() (garden.BandwidthLimits, error) {
	c.logger.Info("vcontainer-current-bandwidth-limits")
	return garden.BandwidthLimits{}, nil
}

func (c *VContainer) CurrentCPULimits() (garden.CPULimits, error) {
	c.logger.Info("vcontainer-current-cpu-limits")
	ctx := c.buildContext()
	limits, err := c.vcontainerClient.CurrentCPULimit(ctx, &google_protobuf.Empty{}, nil)
	if err != nil {
		c.logger.Error("vcontainer-get-current-cpu-limits-failed", err, lager.Data{"container_id": c.handle})
		return garden.CPULimits{}, verrors.New("vcontainer-get-cpu-limits-failed")
	} else {
		return garden.CPULimits{
			LimitInShares: limits.LimitInShares,
		}, nil
	}
}

func (c *VContainer) CurrentDiskLimits() (garden.DiskLimits, error) {
	c.logger.Info("vcontainer-current-disk-limits")
	return garden.DiskLimits{}, nil
}

func (c *VContainer) CurrentMemoryLimits() (garden.MemoryLimits, error) {
	c.logger.Info("vcontainer-current-memory-limits")
	ctx := c.buildContext()
	limits, err := c.vcontainerClient.CurrentMemoryLimit(ctx, &google_protobuf.Empty{}, nil)
	if err != nil {
		c.logger.Error("vcontainer-get-current-cpu-limits-failed", err, lager.Data{"container_id": c.handle})
		return garden.MemoryLimits{}, verrors.New("vcontainer-get-memory-limits-failed")
	} else {
		return garden.MemoryLimits{
			LimitInBytes: limits.LimitInBytes,
		}, nil
	}
}

func (c *VContainer) Run(spec garden.ProcessSpec, io garden.ProcessIO) (garden.Process, error) {
	c.logger.Info("vcontainer-run-spec", lager.Data{"spec": spec, "handle": c.handle})
	ctx := c.buildContext()
	specRemote, _ := ConvertProcessSpec(spec)
	runResponse, err := c.vcontainerClient.Run(ctx, specRemote)
	taskId := ""
	if err != nil {
		c.logger.Error("vcontainer-run-run-failed", err)
		// return nil, err
	} else {
		c.logger.Info("vcontainer-run-spec-result-id", lager.Data{"id": runResponse.ID})
		taskId = runResponse.ID
	}

	process := NewVProcess(c.logger, c.handle, taskId, c.vprocessClient)
	return process, nil
}

func (c *VContainer) Attach(processID string, io garden.ProcessIO) (garden.Process, error) {
	c.logger.Info("vcontainer-attach", lager.Data{"process_id": processID})
	return nil, verrors.New("vcontainer-attach-not-implemented")
}

func (c *VContainer) NetIn(hostPort, cPort uint32) (uint32, uint32, error) {
	c.logger.Info("vcontainer-net-in", lager.Data{"host_port": hostPort, "c_port": cPort})
	return 0, 0, verrors.New("vcontainer-net-in-not-implemented")
}

func (c *VContainer) NetOut(netOutRule garden.NetOutRule) error {
	c.logger.Info("vcontainer-net-out", lager.Data{"rule": netOutRule})
	return verrors.New("vcontainer-net-out-not-implemented")
}

func (c *VContainer) BulkNetOut(netOutRules []garden.NetOutRule) error {
	c.logger.Info("vcontainer-bulk-net-out", lager.Data{"net_out_rules": netOutRules})
	return verrors.New("vcontainer-bulk-net-out-not-implemented")
}

func (c *VContainer) Metrics() (garden.Metrics, error) {
	c.logger.Info("vcontainer-metrics")
	ctx := c.buildContext()
	_, err := c.vcontainerClient.Metrics(ctx, &google_protobuf.Empty{})
	if err != nil {
		c.logger.Error("metrics", err)
	}
	return garden.Metrics{}, nil
}

func (c *VContainer) SetGraceTime(graceTime time.Duration) error {
	c.logger.Info("vcontainer-set-grace-time", lager.Data{"grace_time": graceTime})
	protoDuration := &google_protobuf.Duration{}
	protoDuration.Seconds = int64(graceTime) / 1e9
	protoDuration.Nanos = int32(int64(graceTime) % 1e9)
	ctx := c.buildContext()
	_, err := c.vcontainerClient.SetGraceTime(ctx, protoDuration)
	if err != nil {
		c.logger.Error("set-grace-time-failed", err)
	}
	return err
}

func (c *VContainer) Properties() (garden.Properties, error) {
	c.logger.Info("vcontainer-properties")
	ctx := c.buildContext()
	properties, err := c.vcontainerClient.Properties(ctx, &google_protobuf.Empty{})
	if err != nil {
		c.logger.Error("properties", err)
		return nil, err
	} else {
		return ConvertProperties(properties), nil
	}
}

func (c *VContainer) Property(name string) (string, error) {
	c.logger.Info("vcontainer-property")
	ctx := c.buildContext()
	property, err := c.vcontainerClient.Property(ctx, &google_protobuf.StringValue{
		Value: name,
	})
	if err != nil {
		c.logger.Error("property", err)
		return "", err
	} else {
		return property.Value, nil
	}
}

func (c *VContainer) SetProperty(name string, value string) error {
	c.logger.Info("vcontainer-set-property")
	ctx := c.buildContext()
	_, err := c.vcontainerClient.SetProperty(ctx, &vcontainermodels.KeyValueMessage{
		Key:   name,
		Value: value,
	})
	if err != nil {
		c.logger.Error("set-property", err)
		return err
	}
	return nil
}

func (c *VContainer) RemoveProperty(name string) error {
	c.logger.Info("vcontainer-remove-property")
	ctx := c.buildContext()
	_, err := c.vcontainerClient.RemoveProperty(ctx, &google_protobuf.StringValue{
		Value: name,
	})
	if err != nil {
		c.logger.Error("remove-property", err)
		return err
	}
	return nil
}

func (c *VContainer) buildContext() context.Context {
	md := metadata.Pairs(vcontainercommon.ContainerIDKey, c.Handle())
	ctx := context.Background()
	ctx = metadata.NewContext(ctx, md)
	return ctx
}
