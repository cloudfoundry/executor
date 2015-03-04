package volumes_test

import (
	"github.com/cloudfoundry-incubator/executor/volume"

	"github.com/cloudfoundry-incubator/executor/fakes"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("VolumeManager", func() {
	Describe("Creating a volume", func() {
		It("Returns a CreateStatus with a volume or error", func() {
			volCreator := fakes.NewFakeVolumeCreator()
			volMgr := volumes.NewManager(volCreator, 100)

			res := volMgr.Create(10)
			Expect(res.Error).To(BeNil())
		})

		It("Returns an error if a volume larger than the total capacity is requested", func() {
			volCreator := fakes.NewFakeVolumeCreator()
			volMgr := volumes.NewManager(volCreator, 100)

			res := volMgr.Create(1000)
			Expect(res.Error).To(MatchError("Insufficient capacity"))
		})

		It("Returns an error if a volume larger than the total capacity is requested", func() {
			volCreator := fakes.NewFakeVolumeCreator()
			volMgr := volumes.NewManager(volCreator, 100)

			res := volMgr.Create(10)
			Expect(res.Error).To(BeNil())

			res = volMgr.Create(91)
			Expect(res.Error).To(MatchError("Insufficient capacity"))
		})

	})

})
