package volumes

type VolumeSpec struct {
	DesiredSize int
	DesiredPath string
}

type Creator interface {
	Create(spec VolumeSpec) error
}
