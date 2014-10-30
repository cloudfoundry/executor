package registry_test

import (
	"fmt"
	"time"

	"github.com/cloudfoundry-incubator/executor"
	. "github.com/cloudfoundry-incubator/executor/depot/registry"
	"github.com/cloudfoundry/gunk/timeprovider/faketimeprovider"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Registry", func() {
	var registry Registry
	var initialCapacity Capacity
	var timeProvider *faketimeprovider.FakeTimeProvider

	BeforeEach(func() {
		initialCapacity = Capacity{
			MemoryMB:   100,
			DiskMB:     100,
			Containers: 10,
		}

		timeProvider = faketimeprovider.New(time.Now())
		registry = New(initialCapacity, timeProvider)
	})

	Describe("TotalCapacity", func() {
		It("should always report the initial capacity", func() {
			Ω(registry.TotalCapacity()).Should(Equal(initialCapacity))

			registry.Reserve(executor.Container{
				Guid:     "a-container",
				MemoryMB: 10,
				DiskMB:   20,
			})

			registry.Reserve(executor.Container{
				Guid:     "another-container",
				MemoryMB: 30,
				DiskMB:   70,
			})

			Ω(registry.TotalCapacity()).Should(Equal(initialCapacity))
		})
	})

	Describe("CurrentCapacity", func() {
		It("should always report the available capacity", func() {
			Ω(registry.CurrentCapacity()).Should(Equal(initialCapacity))

			registry.Reserve(executor.Container{
				Guid:     "a-container",
				MemoryMB: 10,
				DiskMB:   20,
			})

			registry.Reserve(executor.Container{
				Guid:     "another-container",
				MemoryMB: 30,
				DiskMB:   70,
			})

			Ω(registry.CurrentCapacity()).Should(Equal(Capacity{
				MemoryMB:   60,
				DiskMB:     10,
				Containers: 8,
			}))
		})
	})

	Describe("Reserving a container", func() {
		var (
			container executor.Container

			reserveErr error
		)

		BeforeEach(func() {
			container = executor.Container{
				Guid:       "a-container",
				RootFSPath: "some-rootfs-path",

				MemoryMB:  50,
				DiskMB:    50,
				CPUWeight: 50,

				Ports: []executor.PortMapping{
					{ContainerPort: 8080, HostPort: 1234},
				},

				Log: executor.LogConfig{
					Guid: "some-log-guid",
				},

				Tags: executor.Tags{"a": "b"},
			}
		})

		JustBeforeEach(func() {
			reserveErr = registry.Reserve(container)
		})

		Context("when there is enough capacity for the container", func() {
			It("succeeds", func() {
				Ω(reserveErr).ShouldNot(HaveOccurred())
			})
		})

		Context("when there is no memory capacity for the container", func() {
			BeforeEach(func() {
				container.MemoryMB = 101
			})

			It("should return an ErrOutOfMemory", func() {
				Ω(reserveErr).Should(MatchError(ErrOutOfMemory))
			})
		})

		Context("when there is no disk capacity for the container", func() {
			BeforeEach(func() {
				container.DiskMB = 101
			})

			It("should return an ErrOutOfDisk", func() {
				Ω(reserveErr).Should(MatchError(ErrOutOfDisk))
			})
		})

		Context("when the container limit has been reached", func() {
			BeforeEach(func() {
				for i := 0; i < 10; i++ {
					err := registry.Reserve(executor.Container{
						Guid:     fmt.Sprintf("another-container-%d", i),
						MemoryMB: 1,
						DiskMB:   1,
					})
					Ω(err).ShouldNot(HaveOccurred())
				}
			})

			It("should return an ErrOutOfContainers", func() {
				Ω(reserveErr).Should(MatchError(ErrOutOfContainers))
			})
		})
	})

	Describe("deleting a container", func() {
		container := executor.Container{
			Guid:     "a-container",
			MemoryMB: 50,
			DiskMB:   100,
		}

		JustBeforeEach(func() {
			registry.Delete(container)
		})

		BeforeEach(func() {
			err := registry.Reserve(container)
			Ω(err).ShouldNot(HaveOccurred())
		})

		It("should free its resources", func() {
			Ω(registry.CurrentCapacity()).Should(Equal(registry.TotalCapacity()))
		})
	})
})
