package containerstore

import (
	"code.cloudfoundry.org/executor"
	"code.cloudfoundry.org/garden"
	"code.cloudfoundry.org/lager/v3"
	"os"
	"path/filepath"
)

type ServiceBindingRootHandler struct {
	containerMountPath string
	bindingRootPath    string
}

func NewServiceBindingRootHandler(
	bindingRoot string,
	containerMountPath string,
) *ServiceBindingRootHandler {
	return &ServiceBindingRootHandler{
		bindingRootPath:    bindingRoot,
		containerMountPath: containerMountPath,
	}
}

func (h *ServiceBindingRootHandler) CreateDir(logger lager.Logger, container executor.Container) ([]garden.BindMount, error) {
	containerDir := filepath.Join(h.bindingRootPath, container.Guid)
	err := os.Mkdir(containerDir, 0755)
	if err != nil {
		return nil, err
	}

	return []garden.BindMount{
		{
			SrcPath: containerDir,
			DstPath: h.containerMountPath,
			Mode:    garden.BindMountModeRO,
			Origin:  garden.BindMountOriginHost,
		},
	}, nil
}

func (h *ServiceBindingRootHandler) RemoveDir(logger lager.Logger, container executor.Container) error {
	return os.RemoveAll(filepath.Join(h.bindingRootPath, container.Guid))
}
