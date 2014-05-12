package initialize_container

import (
	"encoding/json"
	"net/http"
	"sync"

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
	waitGroup             *sync.WaitGroup
	logger                *gosteno.Logger
}

func New(
	containerOwnerName string,
	containerMaxCPUShares uint64,
	wardenClient warden.Client,
	reg registry.Registry,
	waitGroup *sync.WaitGroup,
	logger *gosteno.Logger,
) http.Handler {
	return &handler{
		containerOwnerName:    containerOwnerName,
		containerMaxCPUShares: containerMaxCPUShares,
		wardenClient:          wardenClient,
		registry:              reg,
		waitGroup:             waitGroup,
		logger:                logger,
	}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.waitGroup.Add(1)
	defer h.waitGroup.Done()

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

func (h *handler) limitContainer(reg api.Container, containerClient warden.Container) error {
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
			ByteLimit: uint64(reg.DiskMB * 1024 * 1024),
		})
		if err != nil {
			return err
		}
	}

	if reg.CpuPercent != 0 {
		err := containerClient.LimitCPU(warden.CPULimits{
			LimitInShares: uint64(float64(h.containerMaxCPUShares) * float64(reg.CpuPercent) / 100.0),
		})
		if err != nil {
			return err
		}
	}

	return nil
}
