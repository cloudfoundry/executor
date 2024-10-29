package containerstore

import (
	"fmt"
	"os"
	"path/filepath"

	"code.cloudfoundry.org/executor"
	"code.cloudfoundry.org/garden"
	"code.cloudfoundry.org/lager/v3"
)

//go:generate counterfeiter -o containerstorefakes/fake_volume_mounted_files_handler.go . VolumeMountedFilesImplementor
type VolumeMountedFilesImplementor interface {
	CreateDir(logger lager.Logger, container executor.Container) ([]garden.BindMount, error)
	RemoveDir(logger lager.Logger, container executor.Container) error
}

type VolumeMountedFilesHandler struct {
	containerMountPath string
	volumeMountPath    string
}

func NewVolumeMountedFilesHandler(
	volumeMountPath string,
	containerMountPath string,
) *VolumeMountedFilesHandler {
	return &VolumeMountedFilesHandler{
		volumeMountPath:    volumeMountPath,
		containerMountPath: containerMountPath,
	}
}

func (h *VolumeMountedFilesHandler) CreateDir(logger lager.Logger, container executor.Container) ([]garden.BindMount, error) {
	containerDir := filepath.Join(h.volumeMountPath, container.Guid)
	err := os.Mkdir(containerDir, 0755)
	if err != nil {
		return nil, err
	}

	logger.Info("creating-dir - volume-mounted-files")

	errServiceBinding := h.volumeMountedFilesForServices(logger, containerDir, container)
	if errServiceBinding != nil {
		logger.Error("creating-dir-volume-mounted-files-failed", errServiceBinding)
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

func (h *VolumeMountedFilesHandler) volumeMountedFilesForServices(
	logger lager.Logger,
	containerDir string,
	containers executor.Container,
) error {
	var cleanupFiles []*os.File

	for _, roots := range containers.RunInfo.VolumeMountedFiles {
		dirName := filepath.Dir(roots.Path)
		if dirName == "" {
			err := fmt.Errorf("failed to extract volume-mounted-files directory. format is: /serviceName/fileName")
			logger.Error(" volume-mounted-files directory is required", err, lager.Data{"dirName": "is empty"})

			continue
		}

		fileName := filepath.Base(roots.Path)
		if fileName == "" {
			err := fmt.Errorf("failed to extract service config file. format is: /serviceName/fileName")
			logger.Error("service config file is required", err, lager.Data{"fileName": "is empty"})

			continue
		}

		dirName = filepath.Join(containerDir, dirName)

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

		err = os.WriteFile(filePath, []byte(roots.Content), 0644)
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

func (h *VolumeMountedFilesHandler) RemoveDir(logger lager.Logger, container executor.Container) error {
	path := filepath.Join(h.volumeMountPath, container.Guid)

	err := os.RemoveAll(path)
	if err != nil {
		logger.Error("failed-to-remove-volume-mounted-files-directory", err, lager.Data{"directory": path})
	}

	return err
}
