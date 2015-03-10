package volumes

import (
	"errors"
	"io/ioutil"
	"sync"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/pivotal-golang/lager"
)

type VolumeSpec struct {
	VolumeGuid      string
	DesiredSize     int
	DesiredHostPath string
}

type Volume struct {
	executor.Volume
	HostPath    string
	BackingPath string
}

type Manager interface {
	Create(executor.Volume) error
	Delete(id string) error
	Get(id string) (Volume, error)
	GetAll() []Volume
	TotalCapacityMB() int
	ReservedMemoryMB() int
	AvailableCapacityMB() int
}

type manager struct {
	totalCapacityMB int
	volumeCreator   Creator
	backingStore    string
	mountRootPath   string
	logger          lager.Logger

	*sync.RWMutex
	//TODO: persist these values
	volumes             map[string]Volume
	reservedMemoryMB    int
	availableCapacityMB int
}

func NewManager(logger lager.Logger, store string, creator Creator, capMB int) Manager {
	return &manager{
		logger:          logger,
		volumeCreator:   creator,
		backingStore:    store,
		totalCapacityMB: capMB,
		mountRootPath:   "/tmp",

		RWMutex:             new(sync.RWMutex),
		availableCapacityMB: capMB,
		volumes:             map[string]Volume{},
	}
}

func (vm *manager) Create(volume executor.Volume) error {
	vm.Lock()
	if vm.availableCapacityMB-volume.SizeMB < 0 {
		vm.Unlock()
		enospace := errors.New("Insufficient capacity")
		return enospace
	}

	vm.reservedMemoryMB += volume.ReservedMemoryMB
	vm.availableCapacityMB -= volume.SizeMB

	vm.Unlock()

	//TODO: use mkdir, not tmpdir
	dir, err := ioutil.TempDir(vm.mountRootPath, "mount-"+volume.VolumeGuid)
	if err != nil {
		return err
	}

	spec := VolumeSpec{
		VolumeGuid:      volume.VolumeGuid,
		DesiredSize:     volume.SizeMB,
		DesiredHostPath: dir,
	}

	backingPath, err := vm.volumeCreator.Create(vm.backingStore, spec)
	if err != nil {
		vm.Lock()
		vm.reservedMemoryMB -= volume.ReservedMemoryMB
		vm.availableCapacityMB += volume.SizeMB
		vm.Unlock()

		vm.logger.Error("volumemanager-create", err)
		return err
	}

	vm.Lock()
	vm.volumes[volume.VolumeGuid] = Volume{
		Volume:      volume,
		HostPath:    dir,
		BackingPath: backingPath,
	}
	vm.Unlock()

	return nil
}

func (vm *manager) Get(id string) (Volume, error) {
	vm.Lock()
	defer vm.Unlock()

	v, ok := vm.volumes[id]
	if !ok {
		return Volume{}, errors.New("No such volume found")
	}

	return v, nil
}

func (vm *manager) Delete(id string) error {
	vm.Lock()
	defer vm.Unlock()

	v, ok := vm.volumes[id]
	if !ok {
		return errors.New("No such volume found")
	}

	vm.reservedMemoryMB -= v.ReservedMemoryMB
	vm.availableCapacityMB += v.SizeMB

	delete(vm.volumes, id)
	return nil
}

func (vm *manager) GetAll() []Volume {
	vm.RLock()
	defer vm.RUnlock()

	var volumes []Volume
	for _, v := range vm.volumes {
		volumes = append(volumes, v)
	}

	return volumes
}

func (vm *manager) TotalCapacityMB() int {
	vm.RLock()
	defer vm.RUnlock()
	return vm.totalCapacityMB
}

func (vm *manager) ReservedMemoryMB() int {
	vm.RLock()
	defer vm.RUnlock()
	return vm.reservedMemoryMB
}

func (vm *manager) AvailableCapacityMB() int {
	vm.RLock()
	defer vm.RUnlock()
	return vm.availableCapacityMB
}
