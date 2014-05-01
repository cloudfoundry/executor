package configuration_test

import (
	"errors"
	"github.com/cloudfoundry-incubator/executor/configuration"
	"github.com/cloudfoundry-incubator/garden/client/fake_warden_client"
	"github.com/cloudfoundry-incubator/garden/warden"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("configuration", func() {
	var (
		config       configuration.Configuration
		wardenClient *fake_warden_client.FakeClient
	)

	BeforeEach(func() {
		wardenClient = fake_warden_client.New()

		config = configuration.New(wardenClient)
	})

	Describe("getting the memory limit", func() {
		Context("when the limit flag is 'auto'", func() {
			It("uses the warden server's memory capacity", func() {
				wardenClient.Connection.WhenGettingCapacity = func() (warden.Capacity, error) {
					return warden.Capacity{
						MemoryInBytes: 1024 * 1024 * 3,
					}, nil
				}

				memoryMB, err := config.GetMemoryInMB("auto")
				Ω(err).ShouldNot(HaveOccurred())
				Ω(memoryMB).Should(Equal(3))
			})

			It("does not compute the capacity more than once", func() {
				wardenClient.Connection.WhenGettingCapacity = func() (warden.Capacity, error) {
					return warden.Capacity{
						MemoryInBytes: 1024 * 1024 * 3,
					}, nil
				}
				config.GetMemoryInMB("auto")

				wardenClient.Connection.WhenGettingCapacity = func() (warden.Capacity, error) {
					Fail("Should not compute capacity again!")
					return warden.Capacity{}, nil
				}

				config.GetMemoryInMB("auto")
			})

			Context("when getting the capacity fails", func() {
				BeforeEach(func() {
					wardenClient.Connection.WhenGettingCapacity = func() (warden.Capacity, error) {
						return warden.Capacity{}, errors.New("uh oh")
					}
				})

				It("returns an error", func() {
					_, err := config.GetMemoryInMB("auto")
					Ω(err).Should(Equal(errors.New("uh oh")))
				})
			})
		})

		Context("when the limit flag is not a number", func() {
			It("returns an error", func() {
				_, err := config.GetMemoryInMB("stuff")
				Ω(err).Should(Equal(errors.New("memory limit must be a positive number or 'auto'")))
			})
		})

		Context("when the limit flag is not positive", func() {
			It("returns an error", func() {
				_, err := config.GetMemoryInMB("0")
				Ω(err).Should(Equal(errors.New("memory limit must be a positive number or 'auto'")))
			})
		})

		Context("when the limit flag is a positive number", func() {
			It("uses that number", func() {
				memoryMB, err := config.GetMemoryInMB("2")
				Ω(err).ShouldNot(HaveOccurred())
				Ω(memoryMB).Should(Equal(2))
			})
		})
	})

	Describe("getting the disk limit", func() {
		Context("when the limit flag is 'auto'", func() {
			It("uses the warden server's disk capacity", func() {
				wardenClient.Connection.WhenGettingCapacity = func() (warden.Capacity, error) {
					return warden.Capacity{
						DiskInBytes: 1024 * 1024 * 3,
					}, nil
				}

				diskMB, err := config.GetDiskInMB("auto")
				Ω(err).ShouldNot(HaveOccurred())
				Ω(diskMB).Should(Equal(3))
			})

			Context("when getting the capacity fails", func() {
				BeforeEach(func() {
					wardenClient.Connection.WhenGettingCapacity = func() (warden.Capacity, error) {
						return warden.Capacity{}, errors.New("uh oh")
					}
				})

				It("returns an error", func() {
					_, err := config.GetDiskInMB("auto")
					Ω(err).Should(Equal(errors.New("uh oh")))
				})
			})
		})

		Context("when the limit flag is not a number", func() {
			It("returns an error", func() {
				_, err := config.GetDiskInMB("stuff")
				Ω(err).Should(Equal(errors.New("disk limit must be a positive number or 'auto'")))
			})
		})

		Context("when the limit flag is not positive", func() {
			It("returns an error", func() {
				_, err := config.GetDiskInMB("0")
				Ω(err).Should(Equal(errors.New("disk limit must be a positive number or 'auto'")))
			})
		})

		Context("when the limit flag is a positive number", func() {
			It("uses that number", func() {
				diskMB, err := config.GetDiskInMB("2")
				Ω(err).ShouldNot(HaveOccurred())
				Ω(diskMB).Should(Equal(2))
			})
		})
	})
})
