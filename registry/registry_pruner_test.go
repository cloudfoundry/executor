package registry_test

import (
	"time"

	"github.com/cloudfoundry-incubator/executor/api"
	. "github.com/cloudfoundry-incubator/executor/registry"
	"github.com/cloudfoundry/gunk/timeprovider/faketimeprovider"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("RegistryPruner", func() {

	Describe("Prunes the registry", func() {
		var timeProvider *faketimeprovider.FakeTimeProvider
		var registry Registry
		var registryPruner *RegistryPruner
		var interval time.Duration

		BeforeEach(func() {
			timeProvider = faketimeprovider.New(time.Now())
			timeProvider.ProvideFakeChannels = true
			registry = New("executor-guid", Capacity{
				MemoryMB:   1024,
				DiskMB:     2048,
				Containers: 5,
			}, timeProvider)
			interval = 10 * time.Second
			registryPruner = NewPruner(registry, timeProvider, interval)
			registryPruner.Start()
		})

		JustBeforeEach(func(done Done) {
			timeProvider.TickerChannelFor("pruner") <- timeProvider.Time()
			close(done)
		})

		Context("when a container has been allocated", func() {
			BeforeEach(func() {
				registry.Reserve("container-guid", api.ContainerAllocationRequest{
					MemoryMB: 64,
					DiskMB:   32,
				})
			})

			It("should not remove newly allocated containers", func() {
				Consistently(registry.GetAllContainers).Should(HaveLen(1))
			})

			Context("when a substantial amount of time has passed", func() {
				BeforeEach(func() {
					timeProvider.Increment(interval)
				})

				It("removes old allocated containers", func() {
					Eventually(registry.GetAllContainers).Should(BeEmpty())
				})
			})

			Context("when a container has been initialized and substantial amount of time has passed", func() {
				BeforeEach(func() {
					registry.Create("container-guid", "handle", api.ContainerInitializationRequest{})
					timeProvider.Increment(interval)
				})

				It("should not reap the container", func() {
					Consistently(registry.GetAllContainers).Should(HaveLen(1))
				})
			})
		})
	})

})
