package registry_test

import (
	"fmt"
	"syscall"
	"time"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/executor/depot/allocations"
	"github.com/cloudfoundry-incubator/executor/depot/exchanger"
	. "github.com/cloudfoundry-incubator/executor/depot/registry"
	garden "github.com/cloudfoundry-incubator/garden/api"
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
		var allocationsClient garden.Client
		var exc exchanger.Exchanger

		BeforeEach(func() {
			allocationsClient = allocations.NewClient()
			exc = exchanger.NewExchanger("the-owner", 1024, 2048)

			timeProvider = faketimeprovider.New(time.Now())
			timeProvider.ProvideFakeChannels = true
			registry = New(Capacity{
				MemoryMB:   1024,
				DiskMB:     2048,
				Containers: 5,
			}, timeProvider)
			interval = 10 * time.Second
			process = ifrit.Envoke(NewPruner(registry, allocationsClient, exc, timeProvider, interval, lagertest.NewTestLogger("test")))
		})

		AfterEach(func() {
			process.Signal(syscall.SIGTERM)
			Eventually(process.Wait()).Should(Receive(BeNil()))
		})

		JustBeforeEach(func() {
			Eventually(timeProvider.TickerChannelFor("pruner")).Should(BeSent(timeProvider.Time()))
		})

		Context("when a container has been allocated", func() {
			var allocatedContainer garden.Container

			BeforeEach(func() {
				allocatedAt := timeProvider.Time().UnixNano()

				var err error
				allocatedContainer, err = allocationsClient.Create(garden.ContainerSpec{
					Handle: "some-handle",
					Properties: garden.Properties{
						exchanger.ContainerStateProperty:       string(executor.StateReserved),
						exchanger.ContainerAllocatedAtProperty: fmt.Sprintf("%d", allocatedAt),
					},
				})
				Ω(err).ShouldNot(HaveOccurred())
			})

			allContainers := func() []garden.Container {
				containers, err := allocationsClient.Containers(garden.Properties{})
				Ω(err).ShouldNot(HaveOccurred())
				return containers
			}

			It("should not remove newly allocated containers", func() {
				Consistently(allContainers).Should(HaveLen(1))
			})

			Context("and then starts initializing", func() {
				BeforeEach(func() {
					allocatedContainer.SetProperty(exchanger.ContainerStateProperty, string(executor.StateInitializing))
				})

				It("should not be pruned", func() {
					Consistently(allContainers).Should(HaveLen(1))
				})

				Context("when a substantial amount of time has passed", func() {
					BeforeEach(func() {
						timeProvider.Increment(interval)
					})

					It("should not be pruned", func() {
						Consistently(allContainers).Should(HaveLen(1))
					})
				})
			})

			Context("when a substantial amount of time has passed", func() {
				BeforeEach(func() {
					timeProvider.Increment(interval + 1)
				})

				It("removes old allocated containers", func() {
					Eventually(allContainers).Should(BeEmpty())
				})
			})
		})
	})
})
