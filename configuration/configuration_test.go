package configuration_test

import (
	"errors"

	"github.com/cloudfoundry-incubator/executor/configuration"
	"github.com/cloudfoundry-incubator/executor/registry"
	"github.com/cloudfoundry-incubator/garden/client/fake_warden_client"
	"github.com/cloudfoundry-incubator/garden/warden"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("configuration", func() {
	var wardenClient *fake_warden_client.FakeClient

	BeforeEach(func() {
		wardenClient = fake_warden_client.New()
	})

	Describe("ConfigureCapacity", func() {
		var capacity registry.Capacity
		var err error
		var memLimit string
		var diskLimit string

		JustBeforeEach(func() {
			capacity, err = configuration.ConfigureCapacity(wardenClient, memLimit, diskLimit)
		})

		Context("when getting the capacity fails", func() {
			BeforeEach(func() {
				wardenClient.Connection.CapacityReturns(warden.Capacity{}, errors.New("uh oh"))
			})

			It("returns an error", func() {
				Ω(err).Should(Equal(errors.New("uh oh")))
			})
		})

		Context("when getting the capacity succeeds", func() {
			BeforeEach(func() {
				memLimit = "99"
				diskLimit = "99"
				wardenClient.Connection.CapacityReturns(
					warden.Capacity{
						MemoryInBytes: 1024 * 1024 * 3,
						DiskInBytes:   1024 * 1024 * 4,
						MaxContainers: 5,
					},
					nil,
				)
			})

			Describe("Memory Limit", func() {
				Context("when the memory limit flag is 'auto'", func() {
					BeforeEach(func() {
						memLimit = "auto"
					})

					It("does not return an error", func() {
						Ω(err).ShouldNot(HaveOccurred())
					})

					It("uses the warden server's memory capacity", func() {
						Ω(capacity.MemoryMB).Should(Equal(3))
					})
				})

				Context("when the memory limit flag is a positive number", func() {
					BeforeEach(func() {
						memLimit = "2"
					})

					It("does not return an error", func() {
						Ω(err).ShouldNot(HaveOccurred())
					})

					It("uses that number", func() {
						Ω(capacity.MemoryMB).Should(Equal(2))
					})
				})

				Context("when the memory limit flag is not a number", func() {
					BeforeEach(func() {
						memLimit = "stuff"
					})

					It("returns an error", func() {
						Ω(err).Should(Equal(configuration.ErrMemoryFlagInvalid))
					})
				})

				Context("when the memory limit flag is not positive", func() {
					BeforeEach(func() {
						memLimit = "0"
					})

					It("returns an error", func() {
						Ω(err).Should(Equal(configuration.ErrMemoryFlagInvalid))
					})
				})
			})

			Describe("Disk Limit", func() {
				Context("when the disk limit flag is 'auto'", func() {
					BeforeEach(func() {
						diskLimit = "auto"
					})

					It("does not return an error", func() {
						Ω(err).ShouldNot(HaveOccurred())
					})

					It("uses the warden server's memory capacity", func() {
						Ω(capacity.DiskMB).Should(Equal(4))
					})
				})

				Context("when the disk limit flag is a positive number", func() {
					BeforeEach(func() {
						diskLimit = "2"
					})

					It("does not return an error", func() {
						Ω(err).ShouldNot(HaveOccurred())
					})

					It("uses that number", func() {
						Ω(capacity.DiskMB).Should(Equal(2))
					})
				})

				Context("when the disk limit flag is not a number", func() {
					BeforeEach(func() {
						diskLimit = "stuff"
					})

					It("returns an error", func() {
						Ω(err).Should(Equal(configuration.ErrDiskFlagInvalid))
					})
				})

				Context("when the disk limit flag is not positive", func() {
					BeforeEach(func() {
						diskLimit = "0"
					})

					It("returns an error", func() {
						Ω(err).Should(Equal(configuration.ErrDiskFlagInvalid))
					})
				})
			})

			Describe("Containers Limit", func() {
				It("uses the warden server's max containers", func() {
					Ω(capacity.Containers).Should(Equal(5))
				})
			})
		})
	})
})
