package bbs_test

import (
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	. "github.com/cloudfoundry-incubator/runtime-schema/bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models/factories"
	"github.com/cloudfoundry/gunk/timeprovider/faketimeprovider"
	"github.com/cloudfoundry/storeadapter"
)

var _ = Describe("File Server BBS", func() {
	var (
		bbs           *BBS
		fileServerURL string
		fileServerId  string
		interval      time.Duration
		disappeared   <-chan bool
		err           error
		presence      PresenceInterface
		timeProvider  *faketimeprovider.FakeTimeProvider
	)

	BeforeEach(func() {
		timeProvider = faketimeprovider.New(time.Unix(1238, 0))
		bbs = New(store, timeProvider)
	})

	Describe("MaintainFileServerPresence", func() {
		BeforeEach(func() {
			fileServerURL = "stubFileServerURL"
			fileServerId = factories.GenerateGuid()
			interval = 1 * time.Second

			presence, disappeared, err = bbs.MaintainFileServerPresence(interval, fileServerURL, fileServerId)
			Ω(err).ShouldNot(HaveOccurred())
		})

		AfterEach(func() {
			presence.Remove()
		})

		It("should put /file_server/FILE_SERVER_ID in the store with a TTL", func() {
			node, err := store.Get("/v1/file_server/" + fileServerId)
			Ω(err).ShouldNot(HaveOccurred())
			Ω(node).Should(Equal(storeadapter.StoreNode{
				Key:   "/v1/file_server/" + fileServerId,
				Value: []byte(fileServerURL),
				TTL:   uint64(interval.Seconds()), // move to config one day
			}))
		})
	})

	Describe("GetAvailableFileServer", func() {
		Context("when there are available file servers", func() {
			BeforeEach(func() {
				fileServerURL = "http://128.70.3.29:8012"
				fileServerId = factories.GenerateGuid()
				interval = 1 * time.Second

				presence, disappeared, err = bbs.MaintainFileServerPresence(interval, fileServerURL, fileServerId)
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("should get from /v1/file_server/", func() {
				url, err := bbs.GetAvailableFileServer()
				Ω(err).ShouldNot(HaveOccurred())
				Ω(url).Should(Equal(fileServerURL))
			})
		})

		Context("when there are several available file servers", func() {
			var otherFileServerURL string
			BeforeEach(func() {
				fileServerURL = "http://guy"
				otherFileServerURL = "http://other.guy"

				fileServerId = factories.GenerateGuid()
				otherFileServerId := factories.GenerateGuid()

				interval = 1 * time.Second

				presence, disappeared, err = bbs.MaintainFileServerPresence(interval, fileServerURL, fileServerId)
				Ω(err).ShouldNot(HaveOccurred())

				presence, disappeared, err = bbs.MaintainFileServerPresence(interval, otherFileServerURL, otherFileServerId)
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("should pick one at random", func() {
				result := map[string]bool{}

				for i := 0; i < 10; i++ {
					url, err := bbs.GetAvailableFileServer()
					Ω(err).ShouldNot(HaveOccurred())
					result[url] = true
				}

				Ω(result).Should(HaveLen(2))
				Ω(result).Should(HaveKey(fileServerURL))
				Ω(result).Should(HaveKey(otherFileServerURL))
			})
		})

		Context("when there are none", func() {
			It("should error", func() {
				url, err := bbs.GetAvailableFileServer()
				Ω(err).Should(HaveOccurred())
				Ω(url).Should(BeZero())
			})
		})
	})
})
