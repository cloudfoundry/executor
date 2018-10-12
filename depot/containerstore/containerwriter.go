package containerstore

import (
	"bytes"
	"fmt"
	"path/filepath"

	"code.cloudfoundry.org/garden"
)

type GardenContainerWriter struct {
	Client garden.Client
}

func (w *GardenContainerWriter) WriteFile(containerID, pathInContainer string, content []byte) error {
	container, err := w.Client.Lookup(containerID)
	if err != nil {
		return err
	}

	process, err := container.Run(garden.ProcessSpec{
		Path: "/bin/sh",
		Args: []string{"-c", fmt.Sprintf("mkdir -p %s && cat <&0 > %s", filepath.Dir(pathInContainer), pathInContainer)},
	}, garden.ProcessIO{
		Stdin: bytes.NewBuffer(content),
	})

	if err != nil {
		return err
	}

	exitCode, err := process.Wait()
	if err != nil {
		return err
	}

	if exitCode != 0 {
		return fmt.Errorf("process exited with non-zero code: %d", exitCode)
	}

	return nil
}
