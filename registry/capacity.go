package registry

import (
	"errors"

	"github.com/cloudfoundry-incubator/executor/api/containers"
)

var ErrOutOfDisk = errors.New("out of disk capacity")
var ErrOutOfMemory = errors.New("out of memory capacity")

type Capacity struct {
	MemoryMB int
	DiskMB   int
}

func (c *Capacity) alloc(res containers.Container) error {
	if c.MemoryMB-res.MemoryMB < 0 {
		return ErrOutOfMemory
	}

	if c.DiskMB-res.DiskMB < 0 {
		return ErrOutOfDisk
	}

	c.MemoryMB -= res.MemoryMB
	c.DiskMB -= res.DiskMB

	return nil
}

func (c *Capacity) free(res containers.Container) {
	c.MemoryMB += res.MemoryMB
	c.DiskMB += res.DiskMB
}
