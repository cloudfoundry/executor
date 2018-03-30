package vcontainer

import (
	"context"
	"fmt"

	"code.cloudfoundry.org/garden"
	"code.cloudfoundry.org/lager"
	google_protobuf "github.com/gogo/protobuf/types"
	"github.com/virtualcloudfoundry/vcontainercommon"
	"github.com/virtualcloudfoundry/vcontainercommon/vcontainermodels"
	"google.golang.org/grpc/metadata"
)

type VProcess struct {
	containerID    string
	taskID         string
	logger         lager.Logger
	vprocessClient vcontainermodels.VProcessClient
}

func NewVProcess(logger lager.Logger, containerID, taskID string,
	vprocessClient vcontainermodels.VProcessClient) *VProcess {
	logger.Info("vprocess-new-vprocess", lager.Data{"taskID": taskID, "containerID": containerID})
	return &VProcess{
		containerID:    containerID,
		taskID:         taskID,
		logger:         logger,
		vprocessClient: vprocessClient,
	}
}

func (v *VProcess) ID() string {
	return v.containerID
}

func (v *VProcess) Wait() (int, error) {
	v.logger.Info("vprocess-wait", lager.Data{"taskid": v.taskID, "containerid": v.containerID})

	ctx := v.buildContext()
	client, err := v.vprocessClient.Wait(ctx, &google_protobuf.Empty{})
	if err != nil {
		v.logger.Error("vprocess-wait-failed", err)
	}

	defer func() {
		err = client.CloseSend()
		if err != nil {
			v.logger.Error("vprocess-wait-close-send-failed", err)
		}
	}()

	for {
		v.logger.Info("vprocess-wait-still-waiting", lager.Data{"taskid": v.taskID, "containerid": v.containerID})
		waitResponse, err := client.Recv()
		if err != nil {
			v.logger.Error("vprocess-wait-recv-failed", err, lager.Data{"taskid": v.taskID, "containerid": v.containerID})
			return int(waitResponse.ExitCode), err
		}
		if waitResponse != nil && waitResponse.Exited {
			v.logger.Info("vprocess-wait-status-code", lager.Data{"status": waitResponse.ExitCode})
			return int(waitResponse.ExitCode), nil
		}
	}
}

func (v *VProcess) SetTTY(spec garden.TTYSpec) error {
	v.logger.Info("vprocess-set-tty", lager.Data{"taskid": v.taskID, "containerid": v.containerID})
	ctx := v.buildContext()
	ttyReq := vcontainermodels.TTYSpec{
		WindowSize: vcontainermodels.WindowSize{
			Columns: int32(spec.WindowSize.Columns),
			Rows:    int32(spec.WindowSize.Rows),
		},
	}
	_, err := v.vprocessClient.SetTTY(ctx, &ttyReq)
	return err
}

func (v *VProcess) Signal(sig garden.Signal) error {
	v.logger.Info("vprocess-signal", lager.Data{"taskid": v.taskID, "containerid": v.containerID})
	v.logger.Info("vprocess-signal", lager.Data{"taskid": v.taskID, "containerid": v.containerID, "sig": fmt.Sprintf("%d", sig)})
	ctx := v.buildContext()

	signalReq := vcontainermodels.SignalRequest{
		Signal: vcontainermodels.Signal(sig),
	}

	v.logger.Info("vprocess-signal-req", lager.Data{"taskid": v.taskID, "signalReq": signalReq})
	v.logger.Info("vprocess-signal-req", lager.Data{"taskid": v.taskID, "signalReqSignal": fmt.Sprintf("%d", signalReq.Signal)})
	_, err := v.vprocessClient.Signal(ctx, &signalReq)
	return err
}

func (v *VProcess) buildContext() context.Context {
	md := metadata.Pairs(vcontainercommon.ContainerIDKey, v.containerID,
		vcontainercommon.TaskIDKey, v.taskID)
	ctx := context.Background()
	ctx = metadata.NewContext(ctx, md)
	return ctx
}
