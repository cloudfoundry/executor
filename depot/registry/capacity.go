package registry

import (
	"errors"
	"fmt"

	"github.com/cloudfoundry-incubator/executor"
)

var ErrOutOfDisk = errors.New("out of disk capacity")
var ErrOutOfMemory = errors.New("out of memory capacity")
var ErrOutOfContainers = errors.New("out of containers")

type Capacity struct {
	MemoryMB   int
	DiskMB     int
	Containers int
}

func (c *Capacity) String() string {
	return fmt.Sprintf("Mem: %dMB Disk: %dMB Containers: %d", c.MemoryMB, c.DiskMB, c.Containers)
}

func (c *Capacity) alloc(res executor.Container) error {
	if c.MemoryMB-res.MemoryMB < 0 {
		return ErrOutOfMemory
	}

	if c.DiskMB-res.DiskMB < 0 {
		return ErrOutOfDisk
	}

	if c.Containers-1 < 0 {
		return ErrOutOfContainers
	}

	c.MemoryMB -= res.MemoryMB
	c.DiskMB -= res.DiskMB
	c.Containers--

	return nil
}

func (c *Capacity) free(res executor.Container) {
	c.MemoryMB += res.MemoryMB
	c.DiskMB += res.DiskMB
	c.Containers++
}
