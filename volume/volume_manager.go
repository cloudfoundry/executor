package volumes

import (
	"errors"
	"io/ioutil"

	"github.com/nu7hatch/gouuid"
)

type Volume struct {
	Id            string
	TotalCapacity int
	Path          string
}

type CreateStatus struct {
	Error  error
	Volume Volume
}

type DeleteStatus struct {
	Error error
	Ok    bool
}

type Manager interface {
	Create(sizeMB int) CreateStatus
	//	Delete(id string) chan DeleteStatus
	//	Get(id string) (Volume, error)
	//	GetAll() []Volume
	//	TotalCapacityMB() int
	//	ReservedCapacityMB() int
	//	AvailableCapacityMB() int
}

type VolMgr struct {
	totalCapacityMB     int
	reservedCapacityMB  int
	availableCapacityMB int
	volumeCreator       Creator
	volumes             map[string]Volume
}

func NewManager(creator Creator, capMB int) VolMgr {
	vols := make(map[string]Volume)
	return VolMgr{volumes: vols, volumeCreator: creator, totalCapacityMB: capMB, availableCapacityMB: capMB}
}

func (vm *VolMgr) Create(sizeMB int) CreateStatus {
	//TODO: use locking here
	if vm.availableCapacityMB <= 0 {
		enospace := errors.New("No available capacity")
		return CreateStatus{Error: enospace}
	}

	//TODO: use locking here
	if vm.availableCapacityMB-sizeMB < 0 {
		enospace := errors.New("Insufficient capacity")
		return CreateStatus{Error: enospace}
	}

	//TODO: use a better directory to create stores in
	tmpDir, err := ioutil.TempDir("", "volume-store")
	if err != nil {
		return CreateStatus{Error: err}
	}

	volumeSpec := VolumeSpec{DesiredSize: sizeMB, DesiredPath: tmpDir}
	err = vm.volumeCreator.Create(volumeSpec)
	if err != nil {
		return CreateStatus{Error: err}
	}

	vid, err := uuid.NewV4()
	if err != nil {
		return CreateStatus{Error: err}
	}

	//TODO: use locking here
	volume := Volume{TotalCapacity: sizeMB, Id: vid.String(), Path: tmpDir}
	vm.volumes[vid.String()] = volume

	//TODO: use locking here
	vm.reservedCapacityMB += sizeMB
	vm.availableCapacityMB -= sizeMB

	return CreateStatus{Volume: volume}
}
