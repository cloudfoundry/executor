package initialize_container

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/cloudfoundry-incubator/executor/api/containers"
	"github.com/cloudfoundry-incubator/executor/registry"
	"github.com/cloudfoundry-incubator/garden/warden"
)

type handler struct {
	containerOwnerName    string
	containerMaxCPUShares uint64
	wardenClient          warden.Client
	registry              registry.Registry
}

func New(
	containerOwnerName string,
	containerMaxCPUShares uint64,
	wardenClient warden.Client,
	reg registry.Registry,
) http.Handler {
	return &handler{
		containerOwnerName:    containerOwnerName,
		containerMaxCPUShares: containerMaxCPUShares,
		wardenClient:          wardenClient,
		registry:              reg,
	}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	guid := r.FormValue(":guid")

	reg, err := h.registry.FindByGuid(guid)
	if err != nil {
		log.Println("container not found:", err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	containerClient, err := h.wardenClient.Create(warden.ContainerSpec{
		Properties: warden.Properties{
			"owner": h.containerOwnerName,
		},
	})
	if err != nil {
		log.Println("container create failed:", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	err = h.limitContainer(reg, containerClient)
	if err != nil {
		log.Println("container limit failed:", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	reg, err = h.registry.Create(reg.Guid, containerClient.Handle())
	if err != nil {
		log.Println("registry create failed:", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(reg)
}

func (h *handler) limitContainer(reg containers.Container, containerClient warden.Container) error {
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
