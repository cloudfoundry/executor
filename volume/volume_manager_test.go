package volumes_test

import (
	"github.com/cloudfoundry-incubator/executor/fakes"

	. "github.com/cloudfoundry-incubator/executor/volume"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("VolumeManager", func() {
	Describe("Creating a volume", func() {
		It("Returns a CreateStatus with a volume or error", func() {
			volCreator := fakes.NewFakeVolumeCreator()
			volMgr := NewManager(volCreator, 100)

			res := volMgr.Create(10)
			Expect(res.Error).To(BeNil())
		})

		It("Returns an error if a volume larger than the total capacity is requested", func() {
			volCreator := fakes.NewFakeVolumeCreator()
			volMgr := NewManager(volCreator, 100)

			res := volMgr.Create(1000)
			Expect(res.Error).To(MatchError("Insufficient capacity"))
		})

		It("Returns an error if a volume larger than the total capacity is requested", func() {
			volCreator := fakes.NewFakeVolumeCreator()
			volMgr := NewManager(volCreator, 100)

			res := volMgr.Create(10)
			Expect(res.Error).To(BeNil())

			res = volMgr.Create(91)
			Expect(res.Error).To(MatchError("Insufficient capacity"))
		})

	})

	Describe("Getting a volume", func() {
		It("Returns a volume if the requested volume ID exists", func() {
			volCreator := fakes.NewFakeVolumeCreator()
			volMgr := NewManager(volCreator, 100)

			res := volMgr.Create(10)
			Expect(res.Error).To(BeNil())

			vid := res.Volume.Id
			vol, err := volMgr.Get(vid)
			Expect(err).To(BeNil())
			Expect(vol).To(Equal(res.Volume))

		})

		It("Returns an error if the requested volume ID does not exist", func() {
			volCreator := fakes.NewFakeVolumeCreator()
			volMgr := NewManager(volCreator, 100)

			_, err := volMgr.Get("does-not-exist")
			Expect(err).To(MatchError("No such volume found"))
		})
	})

	Describe("Deleting a volume", func() {
		It("Removes a volume from the collection managed by the Manager", func() {
			volCreator := fakes.NewFakeVolumeCreator()
			volMgr := NewManager(volCreator, 100)

			res := volMgr.Create(10)
			Expect(res.Error).To(BeNil())

			vid := res.Volume.Id

			err := volMgr.Delete(vid)
			Expect(err).To(BeNil())

			_, err = volMgr.Get(vid)
			Expect(err).To(MatchError("No such volume found"))
		})
	})

	Describe("Getting all volumes from the manager", func() {
		It("Returns a slice of volumes", func() {
			volCreator := fakes.NewFakeVolumeCreator()
			volMgr := NewManager(volCreator, 100)

			res := volMgr.Create(10)
			Expect(res.Error).To(BeNil())

			res = volMgr.Create(10)
			Expect(res.Error).To(BeNil())

			res = volMgr.Create(10)
			Expect(res.Error).To(BeNil())

			vols := volMgr.GetAll()
			Expect(vols).To(HaveLen(3))
		})
	})

	Describe("Getting storage capacity and utilization", func() {
		It("Returns updated information", func() {
			volCreator := fakes.NewFakeVolumeCreator()
			volMgr := NewManager(volCreator, 100)
			totalSize := volMgr.TotalCapacityMB()
			Expect(totalSize).To(Equal(100))

			res := volMgr.Create(10)
			Expect(res.Error).To(BeNil())
			available := volMgr.AvailableCapacityMB()
			reserved := volMgr.ReservedCapacityMB()
			Expect(available).To(Equal(90))
			Expect(reserved).To(Equal(10))

			vid := res.Volume.Id
			err := volMgr.Delete(vid)
			Expect(err).To(BeNil())

			available = volMgr.AvailableCapacityMB()
			reserved = volMgr.ReservedCapacityMB()
			Expect(available).To(Equal(100))
			Expect(reserved).To(Equal(0))

		})
	})
})
