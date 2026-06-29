package containerstore

import (
	"path/filepath"

	"code.cloudfoundry.org/executor"
	"code.cloudfoundry.org/garden"
	"code.cloudfoundry.org/lager/v3"
)

// SpiffeSocketMountDir is the in-container path where the host SPIFFE socket
// directory is mounted; the workload socket appears at /run/spiffe/workload.sock.
const SpiffeSocketMountDir = "/run/spiffe"

var _ CredentialHandler = (*SpiffeSocketHandler)(nil)

// SpiffeSocketHandler bind-mounts the host SPIFFE workload socket directory
// read-only into every container and exports SPIFFE_ENDPOINT_SOCKET. It is
// intentionally static: no host directory creation, no rotation.
type SpiffeSocketHandler struct {
	hostSocketDir string // = ExecutorConfig.SpiffeSocketDir (B1 host_socket_dir)
}

func NewSpiffeSocketHandler(hostSocketDir string) *SpiffeSocketHandler {
	return &SpiffeSocketHandler{hostSocketDir: hostSocketDir}
}

func (h *SpiffeSocketHandler) CreateDir(logger lager.Logger, container executor.Container) ([]garden.BindMount, []executor.EnvironmentVariable, error) {
	mounts := []garden.BindMount{{
		SrcPath: h.hostSocketDir,
		DstPath: SpiffeSocketMountDir,
		Mode:    garden.BindMountModeRO,
		Origin:  garden.BindMountOriginHost,
	}}
	envs := []executor.EnvironmentVariable{{
		Name:  "SPIFFE_ENDPOINT_SOCKET",
		Value: "unix://" + filepath.Join(SpiffeSocketMountDir, "workload.sock"),
	}}
	return mounts, envs, nil
}

func (h *SpiffeSocketHandler) RemoveDir(logger lager.Logger, container executor.Container) error {
	return nil
}

func (h *SpiffeSocketHandler) Update(credentials Credentials, container executor.Container) error {
	return nil
}

func (h *SpiffeSocketHandler) Close(invalidCredentials Credentials, container executor.Container) error {
	return nil
}
