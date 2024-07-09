package containerstore

import (
	"code.cloudfoundry.org/executor"
	"code.cloudfoundry.org/garden"
	"code.cloudfoundry.org/lager/v3"
	"fmt"
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
	err := os.MkdirAll(containerDir, 0755)
	if err != nil {
		return nil, err
	}

	logger.Info("creating-dir - service binding root")

	errServiceBinding := h.createBindingRootsForServices(logger, containerDir, container)
	if errServiceBinding != nil {
		logger.Error("creating-dir-service-binding-root-failed", errServiceBinding)
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

func (h *ServiceBindingRootHandler) createBindingRootsForServices(
	logger lager.Logger,
	containerDir string,
	containers executor.Container,
) error {
	var cleanupFiles []*os.File

	for _, roots := range containers.RunInfo.FilesVariables {
		dirName := filepath.Dir(roots.Name)
		if dirName == "" {
			err := fmt.Errorf("failed to extract service bindig directory. format is: /serviceName/fileName")
			logger.Error("service binding directory is required", err, lager.Data{"dirName": "is empty"})

			continue
		}

		fileName := filepath.Base(roots.Name)
		if fileName == "" {
			err := fmt.Errorf("failed to extract service config file. format is: /serviceName/fileName")
			logger.Error("service config file is required", err, lager.Data{"fileName": "is empty"})

			continue
		}

		dirName = filepath.Join(dirName, containerDir)

		err := os.MkdirAll(dirName, 0755)
		if err != nil {
			logger.Error("failed-to-create-directory", err, lager.Data{"dirName": dirName})
			return fmt.Errorf("failed to create directory %s: %w", dirName, err)
		}

		filePath := filepath.Join(dirName, fileName)

		serviceFile, err := os.Create(filePath)
		if err != nil {
			logger.Error("failed-to-create-file", err, lager.Data{"filePath": filePath})
			return fmt.Errorf("failed to create file %s: %w", filePath, err)
		}

		cleanupFiles = append(cleanupFiles, serviceFile)

		err = os.WriteFile(filePath, []byte(roots.Value), 0644)
		if err != nil {
			logger.Error("failed-to-write-to-file", err, lager.Data{"filePath": filePath})
			err := serviceFile.Close()
			if err != nil {
				return err
			}
			return fmt.Errorf("failed to write to file %s: %w", filePath, err)
		}
	}

	for _, file := range cleanupFiles {
		err := file.Close()
		if err != nil {
			logger.Error("failed-to-close-file", err, lager.Data{"filePath": file.Name()})
			return fmt.Errorf("failed to close file %s: %w", file.Name(), err)
		}
	}

	return nil
}

func (h *ServiceBindingRootHandler) RemoveDir(logger lager.Logger, container executor.Container) error {
	return os.RemoveAll(filepath.Join(h.bindingRootPath, container.Guid))
}
