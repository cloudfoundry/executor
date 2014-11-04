package tallyman_test

import (
	"github.com/cloudfoundry-incubator/executor"
	. "github.com/cloudfoundry-incubator/executor/depot/tallyman"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Resources", func() {
	var tallyman *Tallyman

	BeforeEach(func() {
		tallyman = NewTallyman()
	})

	Describe("Allocations", func() {
		Context("when a banana is allocated", func() {
			banana := executor.Container{
				Guid:     "1",
				MemoryMB: 512,
				DiskMB:   512,
			}

			BeforeEach(func() {
				tallyman.Allocate(banana)
			})

			It("is included in the bunch", func() {
				Ω(tallyman.Allocations()).Should(ConsistOf([]executor.Container{
					banana,
				}))
			})

			Context("and then deallocated", func() {
				BeforeEach(func() {
					tallyman.Deallocate(banana.Guid)
				})

				It("is no longer in the bunch", func() {
					Ω(tallyman.Allocations()).Should(BeEmpty())
				})
			})

			Context("and then deinitialized", func() {
				BeforeEach(func() {
					tallyman.Deinitialize(banana.Guid)
				})

				It("remains in the bunch", func() {
					Ω(tallyman.Allocations()).Should(ConsistOf([]executor.Container{
						banana,
					}))
				})
			})

			Context("and then initialized", func() {
				BeforeEach(func() {
					tallyman.Initialize(banana)
				})

				It("is included in the buch", func() {
					Ω(tallyman.Allocations()).Should(ConsistOf([]executor.Container{
						banana,
					}))
				})

				Context("and then deallocated", func() {
					BeforeEach(func() {
						tallyman.Deallocate(banana.Guid)
					})

					It("remains in the bunch", func() {
						Ω(tallyman.Allocations()).Should(ConsistOf([]executor.Container{
							banana,
						}))
					})
				})

				Context("and then deinitialized", func() {
					BeforeEach(func() {
						tallyman.Deinitialize(banana.Guid)
					})

					It("is removed from the bunch", func() {
						Ω(tallyman.Allocations()).Should(BeEmpty())
					})
				})
			})
		})

		Context("when a banana is deinitialized", func() {
		})
	})

	Describe("SyncInitialized", func() {
		// A = allocated banana
		// I = initialized banana
		// numbers = banana guid

		initializedContainers := func() []executor.Container {
			for _, banana := range tallyman.Allocations() {
				tallyman.Deallocate(banana.Guid)
			}

			return tallyman.Allocations()
		}

		Describe("starting with A1, I2, I3", func() {
			BeforeEach(func() {
				tallyman.Allocate(executor.Container{Guid: "1"})
				tallyman.Initialize(executor.Container{Guid: "2"})
				tallyman.Initialize(executor.Container{Guid: "3"})
			})

			Describe("syncing [I2]", func() {
				BeforeEach(func() {
					tallyman.SyncInitialized([]executor.Container{
						{Guid: "2"},
					})
				})

				It("results in [A1, I2]", func() {
					Ω(tallyman.Allocations()).Should(ConsistOf([]executor.Container{
						{Guid: "1"},
						{Guid: "2"},
					}))

					Ω(initializedContainers()).Should(ConsistOf([]executor.Container{
						{Guid: "2"},
					}))
				})
			})

			Describe("syncing [I1, I2]", func() {
				BeforeEach(func() {
					tallyman.SyncInitialized([]executor.Container{
						{Guid: "1"},
						{Guid: "2"},
					})
				})

				It("results in [I1, I2]", func() {
					Ω(tallyman.Allocations()).Should(ConsistOf([]executor.Container{
						{Guid: "1"},
						{Guid: "2"},
					}))

					Ω(initializedContainers()).Should(ConsistOf([]executor.Container{
						{Guid: "1"},
						{Guid: "2"},
					}))
				})
			})
		})

		Describe("starting with A1, A2, A3", func() {
			BeforeEach(func() {
				tallyman.Allocate(executor.Container{Guid: "1"})
				tallyman.Allocate(executor.Container{Guid: "2"})
				tallyman.Allocate(executor.Container{Guid: "3"})
			})

			Describe("syncing []", func() {
				BeforeEach(func() {
					tallyman.SyncInitialized([]executor.Container{})
				})

				It("results in [A1, A2, A3]", func() {
					Ω(tallyman.Allocations()).Should(ConsistOf([]executor.Container{
						{Guid: "1"},
						{Guid: "2"},
						{Guid: "3"},
					}))

					Ω(initializedContainers()).Should(ConsistOf([]executor.Container{}))
				})
			})
		})

		Describe("starting with A1, I2", func() {
			BeforeEach(func() {
				tallyman.Allocate(executor.Container{Guid: "1"})
				tallyman.Initialize(executor.Container{Guid: "2"})
			})

			Describe("syncing [I2, I3]]", func() {
				BeforeEach(func() {
					tallyman.SyncInitialized([]executor.Container{
						{Guid: "2"},
						{Guid: "3"},
					})
				})

				It("results in [A1, I2, I3]", func() {
					Ω(tallyman.Allocations()).Should(ConsistOf([]executor.Container{
						{Guid: "1"},
						{Guid: "2"},
						{Guid: "3"},
					}))

					Ω(initializedContainers()).Should(ConsistOf([]executor.Container{
						{Guid: "2"},
						{Guid: "3"},
					}))
				})
			})
		})
	})
})
