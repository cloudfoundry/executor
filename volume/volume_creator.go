package volumes

import (
	"fmt"
	"io/ioutil"
	"os/exec"
	"strings"
)

type VolumeSpec struct {
	DesiredSize int
	DesiredPath string
}

type Creator interface {
	Create(spec VolumeSpec) error
}

type LoopVolumeCreator struct {
	storeLocation string
}

func NewVolumeCreator() LoopVolumeCreator {
	return LoopVolumeCreator{}
}

func (vc *LoopVolumeCreator) Create(spec VolumeSpec) error {
	//TODO: return a Volume and error to not lose backing file location
	f, err := ioutil.TempFile(vc.storeLocation, "backing")
	if err != nil {
		return err
	}

	of := fmt.Sprintf("of=%s", f.Name())
	count := fmt.Sprintf("count=%d", spec.DesiredSize)

	createBackingCmd := exec.Command("dd", "if=/dev/zero", of, "bs=1MiB", count)
	createLoopDevCmd := exec.Command("sudo", "losetup", "--show", "--find", f.Name())

	err = createBackingCmd.Run()
	if err != nil {
		return err
	}

	b, err := createLoopDevCmd.Output()
	if err != nil {
		return err
	}

	loopDevName := strings.TrimSpace(string(b))
	mkfsCmd := exec.Command("sudo", "mkfs.ext4", loopDevName)
	b, err = mkfsCmd.CombinedOutput()
	if err != nil {
		panic(string(b))
		return err
	}

	mountDevCmd := exec.Command("sudo", "mount", loopDevName, spec.DesiredPath)
	err = mountDevCmd.Run()
	if err != nil {
		return err
	}

	chmodCmd := exec.Command("sudo", "chmod", "777", spec.DesiredPath)
	err = chmodCmd.Run()
	if err != nil {
		return err
	}

	return nil
}
