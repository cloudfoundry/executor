package containerstore

import (
	"os"
	"path"
	"path/filepath"

	"code.cloudfoundry.org/executor"
	"code.cloudfoundry.org/garden"
	"code.cloudfoundry.org/lager"
)

func NewInstanceIdentityHandler(
	logger lager.Logger,
	containerGUID string,
	credDir string,
	containerMountPath string,
) *InstanceIdentityHandler {
	return &InstanceIdentityHandler{
		logger:             logger,
		containerGUID:      containerGUID,
		credDir:            credDir,
		containerMountPath: containerMountPath,
	}
}

type InstanceIdentityHandler struct {
	logger             lager.Logger
	containerGUID      string
	containerMountPath string
	credDir            string
}

func (h *InstanceIdentityHandler) CreateDir(container executor.Container) ([]garden.BindMount, []executor.EnvironmentVariable, error) {
	containerDir := filepath.Join(h.credDir, h.containerGUID)
	err := os.Mkdir(containerDir, 0755)
	if err != nil {
		return nil, nil, err
	}

	return []garden.BindMount{
			{
				SrcPath: containerDir,
				DstPath: h.containerMountPath,
				Mode:    garden.BindMountModeRO,
				Origin:  garden.BindMountOriginHost,
			},
		}, []executor.EnvironmentVariable{
			{Name: "CF_INSTANCE_CERT", Value: path.Join(h.containerMountPath, "instance.crt")},
			{Name: "CF_INSTANCE_KEY", Value: path.Join(h.containerMountPath, "instance.key")},
		}, nil
}

func (h *InstanceIdentityHandler) RemoveCredDir(container executor.Container) error {
	return os.RemoveAll(filepath.Join(h.credDir, h.containerGUID))
}

func (h *InstanceIdentityHandler) Update(cred Credential, container executor.Container) error {
	instanceKeyPath := filepath.Join(h.credDir, h.containerGUID, "instance.key")
	tmpInstanceKeyPath := instanceKeyPath + ".tmp"
	certificatePath := filepath.Join(h.credDir, h.containerGUID, "instance.crt")
	tmpCertificatePath := certificatePath + ".tmp"

	instanceKey, err := os.Create(tmpInstanceKeyPath)
	if err != nil {
		return err
	}
	defer instanceKey.Close()

	certificate, err := os.Create(tmpCertificatePath)
	if err != nil {
		return err
	}
	defer certificate.Close()

	_, err = certificate.Write([]byte(cred.Cert))
	if err != nil {
		return err
	}

	_, err = instanceKey.Write([]byte(cred.Key))
	if err != nil {
		return err
	}

	err = instanceKey.Close()
	if err != nil {
		return err
	}

	err = certificate.Close()
	if err != nil {
		return err
	}

	err = os.Rename(tmpInstanceKeyPath, instanceKeyPath)
	if err != nil {
		return err
	}

	err = os.Rename(tmpCertificatePath, certificatePath)
	if err != nil {
		return err
	}

	return nil
}

func (h *InstanceIdentityHandler) Close(cred Credential, container executor.Container) error {
	return nil
}
