package volumes_test

import (
	"github.com/cloudfoundry-incubator/executor/fakes"

	"io/ioutil"

	. "github.com/cloudfoundry-incubator/executor/volume"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("VolumeManager", func() {
	Describe("Creating a volume", func() {
		It("Returns a volume", func() {
			//TODO: Clean up after ourselves
			store, err := ioutil.TempDir("", "store")
			Expect(err).To(BeNil())

			//TODO: Clean up after ourselves
			backing, err := ioutil.TempDir("", "backing")
			Expect(err).To(BeNil())

			volCreator := fakes.NewFakeVolumeCreator()
			volMgr := NewManager(store, volCreator, 100)

			spec := VolumeSpec{DesiredSize: 10, DesiredHostPath: backing}
			v, err := volMgr.Create(spec)
			Expect(err).To(BeNil())
			Expect(v.Path).To(Equal(backing))
			Expect(v.TotalCapacity).To(Equal(10))
		})

		It("Returns an error if a volume larger than the total capacity is requested", func() {
			//TODO: Clean up after ourselves
			store, err := ioutil.TempDir("", "store")
			Expect(err).To(BeNil())

			//TODO: Clean up after ourselves
			backing, err := ioutil.TempDir("", "backing")
			Expect(err).To(BeNil())

			volCreator := fakes.NewFakeVolumeCreator()
			volMgr := NewManager(store, volCreator, 100)

			spec := VolumeSpec{DesiredSize: 1000, DesiredHostPath: backing}
			_, err = volMgr.Create(spec)
			Expect(err).To(MatchError("Insufficient capacity"))
		})

		It("Returns an error if a volume larger than the available capacity is requested", func() {
			//TODO: Clean up after ourselves
			store, err := ioutil.TempDir("", "store")
			Expect(err).To(BeNil())

			//TODO: Clean up after ourselves
			backing, err := ioutil.TempDir("", "backing")
			Expect(err).To(BeNil())

			volCreator := fakes.NewFakeVolumeCreator()
			volMgr := NewManager(store, volCreator, 100)

			spec := VolumeSpec{DesiredSize: 10, DesiredHostPath: backing}
			_, err = volMgr.Create(spec)
			Expect(err).To(BeNil())

			backing, err = ioutil.TempDir("", "backing")
			Expect(err).To(BeNil())

			spec = VolumeSpec{DesiredSize: 91, DesiredHostPath: backing}
			_, err = volMgr.Create(spec)
			Expect(err).To(MatchError("Insufficient capacity"))
		})

	})

	Describe("Getting a volume", func() {
		It("Returns a volume if the requested volume ID exists", func() {
			//TODO: Clean up after ourselves
			store, err := ioutil.TempDir("", "store")
			Expect(err).To(BeNil())

			//TODO: Clean up after ourselves
			backing, err := ioutil.TempDir("", "backing")
			Expect(err).To(BeNil())

			volCreator := fakes.NewFakeVolumeCreator()
			volMgr := NewManager(store, volCreator, 100)

			spec := VolumeSpec{DesiredSize: 10, DesiredHostPath: backing}
			v, err := volMgr.Create(spec)
			Expect(err).To(BeNil())

			vid := v.Id
			vol, err := volMgr.Get(vid)
			Expect(err).To(BeNil())
			Expect(vol).To(Equal(v))

		})

		It("Returns an error if the requested volume ID does not exist", func() {
			//TODO: Clean up after ourselves
			store, err := ioutil.TempDir("", "store")
			Expect(err).To(BeNil())

			volCreator := fakes.NewFakeVolumeCreator()
			volMgr := NewManager(store, volCreator, 100)

			_, err = volMgr.Get("does-not-exist")
			Expect(err).To(MatchError("No such volume found"))
		})
	})

	Describe("Deleting a volume", func() {
		It("Removes a volume from the collection managed by the Manager", func() {
			//TODO: Clean up after ourselves
			store, err := ioutil.TempDir("", "store")
			Expect(err).To(BeNil())

			//TODO: Clean up after ourselves
			backing, err := ioutil.TempDir("", "backing")
			Expect(err).To(BeNil())

			volCreator := fakes.NewFakeVolumeCreator()
			volMgr := NewManager(store, volCreator, 100)

			spec := VolumeSpec{DesiredSize: 10, DesiredHostPath: backing}

			v, err := volMgr.Create(spec)
			Expect(err).To(BeNil())

			vid := v.Id

			err = volMgr.Delete(vid)
			Expect(err).To(BeNil())

			_, err = volMgr.Get(vid)
			Expect(err).To(MatchError("No such volume found"))
		})
	})

	Describe("Getting all volumes from the manager", func() {
		It("Returns a slice of volumes", func() {
			//TODO: Clean up after ourselves
			store, err := ioutil.TempDir("", "store")
			Expect(err).To(BeNil())

			//TODO: Clean up after ourselves
			backing1, err := ioutil.TempDir("", "backing")
			Expect(err).To(BeNil())

			backing2, err := ioutil.TempDir("", "backing")
			Expect(err).To(BeNil())

			backing3, err := ioutil.TempDir("", "backing")
			Expect(err).To(BeNil())

			volCreator := fakes.NewFakeVolumeCreator()
			volMgr := NewManager(store, volCreator, 100)

			spec := VolumeSpec{DesiredSize: 10, DesiredHostPath: backing1}
			v1, err := volMgr.Create(spec)
			Expect(err).To(BeNil())

			spec = VolumeSpec{DesiredSize: 10, DesiredHostPath: backing2}
			v2, err := volMgr.Create(spec)
			Expect(err).To(BeNil())

			spec = VolumeSpec{DesiredSize: 10, DesiredHostPath: backing3}
			v3, err := volMgr.Create(spec)
			Expect(err).To(BeNil())

			vols := volMgr.GetAll()
			Expect(vols).To(HaveLen(3))
			Expect(vols).To(ContainElement(v1))
			Expect(vols).To(ContainElement(v2))
			Expect(vols).To(ContainElement(v3))
		})
	})

	Describe("Getting storage capacity and utilization", func() {
		It("Returns updated information", func() {
			//TODO: Clean up after ourselves
			store, err := ioutil.TempDir("", "store")
			Expect(err).To(BeNil())

			//TODO: Clean up after ourselves
			backing, err := ioutil.TempDir("", "backing")
			Expect(err).To(BeNil())

			volCreator := fakes.NewFakeVolumeCreator()
			volMgr := NewManager(store, volCreator, 100)
			totalSize := volMgr.TotalCapacityMB()
			Expect(totalSize).To(Equal(100))

			spec := VolumeSpec{DesiredSize: 10, DesiredHostPath: backing}
			v, err := volMgr.Create(spec)
			Expect(err).To(BeNil())

			available := volMgr.AvailableCapacityMB()
			reserved := volMgr.ReservedCapacityMB()
			Expect(available).To(Equal(90))
			Expect(reserved).To(Equal(10))

			vid := v.Id
			err = volMgr.Delete(vid)
			Expect(err).To(BeNil())

			available = volMgr.AvailableCapacityMB()
			reserved = volMgr.ReservedCapacityMB()
			Expect(available).To(Equal(100))
			Expect(reserved).To(Equal(0))

		})
	})
})
