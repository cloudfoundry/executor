package gardenstore_test

import (
	"errors"

	"github.com/cloudfoundry-incubator/executor/depot/gardenstore"
	gfakes "github.com/cloudfoundry-incubator/garden/fakes"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("GardenContainerStore", func() {
	var (
		fakeGardenClient *gfakes.FakeClient
		gardenStore      *gardenstore.GardenStore
	)

	BeforeEach(func() {
		fakeGardenClient = new(gfakes.FakeClient)
		gardenStore = gardenstore.NewGardenStore(fakeGardenClient)
	})

	Describe("Ping", func() {
		Context("when pinging succeeds", func() {
			It("succeeds", func() {
				err := gardenStore.Ping()
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("when pinging fails", func() {
			disaster := errors.New("welp")

			BeforeEach(func() {
				fakeGardenClient.PingReturns(disaster)
			})

			It("returns a container-not-found error", func() {
				Expect(gardenStore.Ping()).To(Equal(disaster))
			})
		})
	})
})
