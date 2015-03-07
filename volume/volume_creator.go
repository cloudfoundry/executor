package volumes

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os/exec"
	"strings"

	"github.com/nu7hatch/gouuid"
	"github.com/pivotal-golang/lager"
)

type Creator interface {
	Create(store string, spec VolumeSpec) (Volume, error)
}

type LoopVolumeCreator struct {
	storeLocation string
	logger        lager.Logger
}

func NewCreator(logger lager.Logger) LoopVolumeCreator {
	return LoopVolumeCreator{logger: logger.Session("volume-creator")}
}

func (vc LoopVolumeCreator) Create(store string, spec VolumeSpec) (Volume, error) {
	f, err := ioutil.TempFile(store, "backing")
	if err != nil {
		vc.logger.Error("create-backing-file", err)
		return Volume{}, err
	}

	of := fmt.Sprintf("of=%s", f.Name())
	count := fmt.Sprintf("count=%d", spec.DesiredSize)

	createBackingCmd := exec.Command("dd", "if=/dev/zero", of, "bs=1MiB", count)
	createLoopDevCmd := exec.Command("sudo", "losetup", "--show", "-f", f.Name())

	b, err := createBackingCmd.CombinedOutput()
	if err != nil {
		e := errors.New(string(b))
		vc.logger.Error("create-backing", e)
		return Volume{}, e
	}

	b, err = createLoopDevCmd.CombinedOutput()
	if err != nil {
		e := errors.New(string(b))
		vc.logger.Error("create-loop-device", e)
		return Volume{}, e
	}

	loopDevName := strings.TrimSpace(string(b))
	mkfsCmd := exec.Command("sudo", "mkfs.ext4", loopDevName)
	b, err = mkfsCmd.CombinedOutput()
	if err != nil {
		e := errors.New(string(b))
		vc.logger.Error("create-filesystem", e)
		return Volume{}, e
	}

	mountDevCmd := exec.Command("sudo", "mount", loopDevName, spec.DesiredHostPath)
	b, err = mountDevCmd.CombinedOutput()
	if err != nil {
		e := errors.New(string(b))
		vc.logger.Error("mount-loop-device", e)
		return Volume{}, err
	}

	//TODO: Set less silly perms
	chmodCmd := exec.Command("sudo", "chmod", "777", spec.DesiredHostPath)
	b, err = chmodCmd.CombinedOutput()
	if err != nil {
		e := errors.New(string(b))
		vc.logger.Error("change-permissions", e)
		return Volume{}, err
	}

	id, err := uuid.NewV4()
	if err != nil {
		return Volume{}, err
	}

	v := Volume{
		Id:            id.String(),
		TotalCapacity: spec.DesiredSize,
		Path:          spec.DesiredHostPath,
		Backing:       f.Name(),
	}

	return v, nil
}
