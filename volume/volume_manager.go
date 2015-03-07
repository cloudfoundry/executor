package volumes

import (
	"errors"
	"github.com/pivotal-golang/lager"
	"io/ioutil"
)

type VolumeSpec struct {
	DesiredSize     int
	DesiredHostPath string
}

type Volume struct {
	Id            string
	TotalCapacity int
	Path          string
	Backing       string
}

type Manager interface {
	Create(sizeMB int) (Volume, error)
	Delete(id string) error
	Get(id string) (Volume, error)
	GetAll() []Volume
	TotalCapacityMB() int
	ReservedCapacityMB() int
	AvailableCapacityMB() int
}

type manager struct {
	totalCapacityMB     int
	reservedCapacityMB  int
	availableCapacityMB int
	volumeCreator       Creator
	volumes             map[string]Volume
	backingStore        string
	mountRootPath       string
	logger              lager.Logger
}

func NewManager(logger lager.Logger, store string, creator Creator, capMB int) Manager {
	vols := make(map[string]Volume)
	return &manager{
		logger:              logger,
		volumes:             vols,
		volumeCreator:       creator,
		backingStore:        store,
		totalCapacityMB:     capMB,
		availableCapacityMB: capMB,
		mountRootPath:       "/tmp",
	}
}

func (vm *manager) Create(volumeSizeMB int) (Volume, error) {
	//TODO: use locking here
	if vm.availableCapacityMB <= 0 {
		enospace := errors.New("No available capacity")
		return Volume{}, enospace
	}

	//TODO: use locking here
	if vm.availableCapacityMB-volumeSizeMB < 0 {
		enospace := errors.New("Insufficient capacity")
		return Volume{}, enospace
	}

	dir, err := ioutil.TempDir(vm.mountRootPath, "mount")
	if err != nil {
		return Volume{}, err
	}

	s := VolumeSpec{
		DesiredSize:     volumeSizeMB,
		DesiredHostPath: dir,
	}

	v, err := vm.volumeCreator.Create(vm.backingStore, s)
	if err != nil {
		vm.logger.Error("volumemanager-create", err)
		return Volume{}, err
	}

	//TODO: use locking here
	vm.volumes[v.Id] = v

	//TODO: use locking here
	//TODO: persist these values
	vm.reservedCapacityMB += volumeSizeMB
	vm.availableCapacityMB -= volumeSizeMB

	return v, nil
}

func (vm *manager) Get(id string) (Volume, error) {
	v, ok := vm.volumes[id]
	if !ok {
		//TODO: return typed error
		return Volume{}, errors.New("No such volume found")
	}

	return v, nil
}

func (vm *manager) Delete(id string) error {
	//TODO: do all of this atomically
	v, ok := vm.volumes[id]
	if !ok {
		//TODO: return typed error
		return errors.New("No such volume found")
	}

	vm.reservedCapacityMB -= v.TotalCapacity
	vm.availableCapacityMB += v.TotalCapacity

	delete(vm.volumes, id)
	return nil
}

func (vm *manager) GetAll() []Volume {
	var volumes []Volume
	for _, v := range vm.volumes {
		volumes = append(volumes, v)
	}

	return volumes
}

func (vm *manager) TotalCapacityMB() int {
	//TODO: read this from persistent settings
	return vm.totalCapacityMB
}

func (vm *manager) ReservedCapacityMB() int {
	//TODO: read this from persistent settings
	return vm.reservedCapacityMB
}

func (vm *manager) AvailableCapacityMB() int {
	//TODO: read this from persistent settings
	return vm.availableCapacityMB
}
