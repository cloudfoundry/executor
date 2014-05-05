package initialize_container

import (
	"encoding/json"
	"net/http"
	"github.com/cloudfoundry-incubator/executor/registry"
	"github.com/cloudfoundry-incubator/garden/warden"
	"github.com/cloudfoundry-incubator/runtime-schema/models/executor_api"
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

	err = h.limitContainer(reg, containerClient)
	if err != nil {
		h.logger.Errord(map[string]interface{}{
			"error": err.Error(),
		}, "executor.init-container.limit-failed")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	reg, err = h.registry.Create(reg.Guid, containerClient.Handle())
	if err != nil {
		h.logger.Errord(map[string]interface{}{
			"error": err.Error(),
		}, "executor.init-container.registry-failed")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(reg)
}

func (h *handler) limitContainer(reg executor_api.Container, containerClient warden.Container) error {
	err := containerClient.LimitMemory(warden.MemoryLimits{
		LimitInBytes: uint64(reg.MemoryMB * 1024 * 1024),
	})
	if err != nil {
		return err
	}

	err = containerClient.LimitDisk(warden.DiskLimits{
		ByteLimit: uint64(reg.DiskMB * 1024 * 1024),
	})
	if err != nil {
		return err
	}

	err = containerClient.LimitCPU(warden.CPULimits{
		LimitInShares: uint64(float64(h.containerMaxCPUShares) * reg.CpuPercent),
	})
	if err != nil {
		return err
	}
	return nil
}
