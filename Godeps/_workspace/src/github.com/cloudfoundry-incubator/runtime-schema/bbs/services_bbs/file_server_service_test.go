package services_bbs_test

import (
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	. "github.com/cloudfoundry-incubator/runtime-schema/bbs/services_bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models/factories"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/cloudfoundry/storeadapter/test_helpers"
)

var _ = Describe("Fetching available file servers", func() {
	var (
		bbs           *ServicesBBS
		fileServerURL string
		fileServerId  string
		interval      time.Duration
		status        <-chan bool
		err           error
		presence      Presence
	)

	BeforeEach(func() {
		logSink := steno.NewTestingSink()

		steno.Init(&steno.Config{
			Sinks: []steno.Sink{logSink},
		})

		logger := steno.NewLogger("the-logger")
		steno.EnterTestMode()
		bbs = New(etcdClient, logger)
	})

	Describe("GetAvailableFileServer", func() {
		Context("when there are available file servers", func() {
			BeforeEach(func() {
				fileServerURL = "http://128.70.3.29:8012"
				fileServerId = factories.GenerateGuid()
				interval = 1 * time.Second

				presence, status, err = bbs.MaintainFileServerPresence(interval, fileServerURL, fileServerId)
				Ω(err).ShouldNot(HaveOccurred())

				reporter := test_helpers.NewStatusReporter(status)
				Eventually(reporter.Locked).Should(BeTrue())
			})

			AfterEach(func() {
				presence.Remove()
			})

			It("should get from /v1/file_server/", func() {
				url, err := bbs.GetAvailableFileServer()
				Ω(err).ShouldNot(HaveOccurred())
				Ω(url).Should(Equal(fileServerURL))
			})
		})

		Context("when there are several available file servers", func() {
			var (
				otherFileServerURL string
				otherPresence      Presence
			)

			BeforeEach(func() {
				fileServerURL = "http://guy"
				otherFileServerURL = "http://other.guy"

				fileServerId = factories.GenerateGuid()
				otherFileServerId := factories.GenerateGuid()

				interval = 1 * time.Second

				presence, status, err = bbs.MaintainFileServerPresence(interval, fileServerURL, fileServerId)
				Ω(err).ShouldNot(HaveOccurred())

				reporter := test_helpers.NewStatusReporter(status)

				otherPresence, status, err = bbs.MaintainFileServerPresence(interval, otherFileServerURL, otherFileServerId)

				Ω(err).ShouldNot(HaveOccurred())
				otherReporter := test_helpers.NewStatusReporter(status)

				Eventually(reporter.Locked).Should(BeTrue())
				Eventually(otherReporter.Locked).Should(BeTrue())
			})

			AfterEach(func() {
				presence.Remove()
				otherPresence.Remove()
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
