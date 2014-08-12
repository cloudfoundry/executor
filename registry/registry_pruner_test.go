package registry_test

import (
	"syscall"
	"time"

	"github.com/cloudfoundry-incubator/executor/api"
	. "github.com/cloudfoundry-incubator/executor/registry"
	"github.com/cloudfoundry/gunk/timeprovider/faketimeprovider"
	"github.com/pivotal-golang/lager/lagertest"
	"github.com/tedsuo/ifrit"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("RegistryPruner", func() {
	Describe("Prunes the registry", func() {
		var timeProvider *faketimeprovider.FakeTimeProvider
		var registry Registry
		var process ifrit.Process
		var interval time.Duration

		BeforeEach(func() {
			timeProvider = faketimeprovider.New(time.Now())
			timeProvider.ProvideFakeChannels = true
			registry = New(Capacity{
				MemoryMB:   1024,
				DiskMB:     2048,
				Containers: 5,
			}, timeProvider)
			interval = 10 * time.Second
			process = ifrit.Envoke(NewPruner(registry, timeProvider, interval, lagertest.NewTestLogger("test")))
		})

		AfterEach(func() {
			process.Signal(syscall.SIGTERM)
			Eventually(process.Wait()).Should(Receive(BeNil()))
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
					_, err := registry.Initialize("container-guid")
					Ω(err).ShouldNot(HaveOccurred())
					timeProvider.Increment(interval)
				})

				It("should not reap the container", func() {
					Consistently(registry.GetAllContainers).Should(HaveLen(1))
				})
			})
		})
	})
})
