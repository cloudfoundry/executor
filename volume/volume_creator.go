package volumes

import (
	"fmt"
	"io/ioutil"
	"os/exec"
	"strings"

	"github.com/nu7hatch/gouuid"
)

type Creator interface {
	Create(store string, spec VolumeSpec) (Volume, error)
}

type LoopVolumeCreator struct {
	storeLocation string
}

func NewCreator() LoopVolumeCreator {
	return LoopVolumeCreator{}
}

func (vc LoopVolumeCreator) Create(store string, spec VolumeSpec) (Volume, error) {
	f, err := ioutil.TempFile(store, "backing")
	if err != nil {
		return Volume{}, err
	}

	of := fmt.Sprintf("of=%s", f.Name())
	count := fmt.Sprintf("count=%d", spec.DesiredSize)

	createBackingCmd := exec.Command("dd", "if=/dev/zero", of, "bs=1MiB", count)
	createLoopDevCmd := exec.Command("sudo", "losetup", "--show", "-f", f.Name())

	err = createBackingCmd.Run()
	if err != nil {
		return Volume{}, err
	}

	b, err := createLoopDevCmd.Output()
	if err != nil {
		return Volume{}, err
	}

	loopDevName := strings.TrimSpace(string(b))
	mkfsCmd := exec.Command("sudo", "mkfs.ext4", loopDevName)
	b, err = mkfsCmd.CombinedOutput()
	if err != nil {
		return Volume{}, err
	}

	mountDevCmd := exec.Command("sudo", "mount", loopDevName, spec.DesiredHostPath)
	err = mountDevCmd.Run()
	if err != nil {
		return Volume{}, err
	}

	//TODO: Set less silly perms
	chmodCmd := exec.Command("sudo", "chmod", "777", spec.DesiredHostPath)
	err = chmodCmd.Run()
	if err != nil {
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
