package volumes_test

import (
	"io/ioutil"
	"path/filepath"

	. "github.com/cloudfoundry-incubator/executor/volume"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("VolumeCreator", func() {
	Describe("Creating a volume from a loop device", func() {
		It("Backs a given directory with a loop device", func() {
			dir, err := ioutil.TempDir("", "volume")
			Expect(err).To(BeNil())

			volCreator := NewVolumeCreator()
			spec := VolumeSpec{DesiredPath: dir, DesiredSize: 100}
			err = volCreator.Create(spec)
			Expect(err).To(BeNil())

			f, err := ioutil.TempFile(dir, "interesting-file")
			Expect(err).To(BeNil())

			f.Write([]byte("some particularlly interesting text"))
			f.Close()

			infos, err := ioutil.ReadDir(dir)
			Expect(err).To(BeNil())

			dirContents := []string{}
			for _, entry := range infos {
				dirContents = append(dirContents, entry.Name())
			}

			Expect(dirContents).To(ContainElement(filepath.Base(f.Name())))
		})
	})
})
