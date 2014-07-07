package initialize_container

import (
	"encoding/json"
	"net/http"

	"github.com/cloudfoundry-incubator/executor/api"
	"github.com/cloudfoundry-incubator/executor/registry"
	"github.com/cloudfoundry-incubator/garden/warden"
	"github.com/cloudfoundry/gosteno"
)

type handler struct {
	containerOwnerName    string
	containerMaxCPUShares uint64
	wardenClient          warden.Client
	registry              registry.Registry
	logger                *gosteno.Logger
}

func New(
	containerOwnerName string,
	containerMaxCPUShares uint64,
	wardenClient warden.Client,
	reg registry.Registry,
	logger *gosteno.Logger,
) http.Handler {
	return &handler{
		containerOwnerName:    containerOwnerName,
		containerMaxCPUShares: containerMaxCPUShares,
		wardenClient:          wardenClient,
		registry:              reg,
		logger:                logger,
	}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	req := api.ContainerInitializationRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		h.logger.Infod(map[string]interface{}{
			"error": err.Error(),
		}, "executor.initialize-container.bad-request")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if req.CpuPercent > 100 || req.CpuPercent < 0 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	guid := r.FormValue(":guid")

	reg, err := h.registry.FindByGuid(guid)
	if err != nil {
		h.logger.Infod(map[string]interface{}{
			"error": err.Error(),
		}, "executor.init-container.not-found")
		w.WriteHeader(http.StatusNotFound)
		return
	}

	containerClient, err := h.wardenClient.Create(warden.ContainerSpec{
		Properties: warden.Properties{
			"owner": h.containerOwnerName,
		},
	})
	if err != nil {
		h.logger.Errord(map[string]interface{}{
			"error": err.Error(),
		}, "executor.init-container.create-failed")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	err = h.limitContainerDiskAndMemory(reg, containerClient)
	if err != nil {
		h.logger.Errord(map[string]interface{}{
			"error": err.Error(),
		}, "executor.init-container.limit-disk-and-memory-failed")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	err = h.limitContainerCPU(req, containerClient)
	if err != nil {
		h.logger.Errord(map[string]interface{}{
			"error": err.Error(),
		}, "executor.init-container.limit-cpu-failed")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	portMapping, err := h.mapPorts(req, containerClient)
	if err != nil {
		h.logger.Errord(map[string]interface{}{
			"error": err.Error(),
		}, "executor.init-container.port-mapping-failed")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	req.Ports = portMapping

	reg, err = h.registry.Create(reg.Guid, containerClient.Handle(), req)
	if err != nil {
		h.logger.Errord(map[string]interface{}{
			"error": err.Error(),
		}, "executor.init-container.registry-failed")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	err = json.NewEncoder(w).Encode(reg)
	if err != nil {
		h.logger.Errord(map[string]interface{}{
			"error": err.Error(),
		}, "executor.init-container.writing-body-failed")
		return
	}

	h.logger.Info("executor.init-container.ok")
}

func (h *handler) limitContainerDiskAndMemory(reg api.Container, containerClient warden.Container) error {
	if reg.MemoryMB != 0 {
		err := containerClient.LimitMemory(warden.MemoryLimits{
			LimitInBytes: uint64(reg.MemoryMB * 1024 * 1024),
		})
		if err != nil {
			return err
		}
	}

	if reg.DiskMB != 0 {
		err := containerClient.LimitDisk(warden.DiskLimits{
			ByteHard: uint64(reg.DiskMB * 1024 * 1024),
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func (h *handler) limitContainerCPU(req api.ContainerInitializationRequest, containerClient warden.Container) error {
	if req.CpuPercent != 0 {
		err := containerClient.LimitCPU(warden.CPULimits{
			LimitInShares: uint64(float64(h.containerMaxCPUShares) * float64(req.CpuPercent) / 100.0),
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func (h *handler) mapPorts(req api.ContainerInitializationRequest, containerClient warden.Container) ([]api.PortMapping, error) {
	var result []api.PortMapping
	for _, mapping := range req.Ports {
		hostPort, containerPort, err := containerClient.NetIn(mapping.HostPort, mapping.ContainerPort)
		if err != nil {
			return nil, err
		}

		result = append(result, api.PortMapping{
			HostPort:      hostPort,
			ContainerPort: containerPort,
		})
	}

	return result, nil
}
