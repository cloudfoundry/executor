package services_bbs_test

import (
	"encoding/json"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"time"

	. "github.com/cloudfoundry-incubator/runtime-schema/bbs/services_bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry-incubator/runtime-schema/models/factories"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/cloudfoundry/storeadapter"
	. "github.com/cloudfoundry/storeadapter/storenodematchers"
	"github.com/cloudfoundry/storeadapter/test_helpers"
)

var _ = Describe("Services BBS", func() {
	var (
		bbs      *ServicesBBS
		presence Presence
		err      error
		status   <-chan bool
		interval time.Duration
		reporter *test_helpers.StatusReporter
	)

	BeforeEach(func() {
		err = nil
		logSink := steno.NewTestingSink()

		steno.Init(&steno.Config{
			Sinks: []steno.Sink{logSink},
		})

		logger := steno.NewLogger("the-logger")
		steno.EnterTestMode()

		bbs = New(etcdClient, logger)
	})

	Describe("MaintainExecutorPresence", func() {
		var executorId string

		BeforeEach(func() {
			executorId = "stubExecutor"
			interval = 1 * time.Second

			presence, status, err = bbs.MaintainExecutorPresence(interval, executorId)
			Ω(err).ShouldNot(HaveOccurred())

			reporter = test_helpers.NewStatusReporter(status)
		})

		AfterEach(func() {
			presence.Remove()
		})

		It("should put /executor/EXECUTOR_ID in the store with a TTL", func() {
			Eventually(reporter.Locked).Should(BeTrue())

			node, err := etcdClient.Get("/v1/executor/" + executorId)
			Ω(err).ShouldNot(HaveOccurred())
			Ω(node.Key).Should(Equal("/v1/executor/" + executorId))
			Ω(node.TTL).Should(Equal(uint64(interval.Seconds()))) // move to config one day
		})
	})

	Describe("MaintainRepPresence", func() {
		var (
			repPresence models.RepPresence
		)

		BeforeEach(func() {
			repPresence = models.RepPresence{
				RepID: "stubRep",
				Stack: "pancakes",
			}
			interval = 1 * time.Second

			presence, status, err = bbs.MaintainRepPresence(interval, repPresence)
			Ω(err).ShouldNot(HaveOccurred())

			reporter = test_helpers.NewStatusReporter(status)
		})

		AfterEach(func() {
			presence.Remove()
		})

		It("should put /executor/EXECUTOR_ID in the store with a TTL", func() {
			Eventually(reporter.Locked).Should(BeTrue())

			node, err := etcdClient.Get("/v1/rep/" + repPresence.RepID)
			Ω(err).ShouldNot(HaveOccurred())
			Ω(node.TTL).Should(Equal(uint64(interval.Seconds()))) // move to config one day

			jsonEncoded, _ := json.Marshal(repPresence)
			Ω(node.Value).Should(MatchJSON(jsonEncoded))
		})
	})

	Describe("MaintainFileServerPresence", func() {
		var fileServerURL string
		var fileServerId string

		BeforeEach(func() {
			fileServerURL = "stubFileServerURL"
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

		It("should put /file_server/FILE_SERVER_ID in the store with a TTL", func() {
			node, err := etcdClient.Get("/v1/file_server/" + fileServerId)
			Ω(err).ShouldNot(HaveOccurred())
			Ω(node).Should(MatchStoreNode(storeadapter.StoreNode{
				Key:   "/v1/file_server/" + fileServerId,
				Value: []byte(fileServerURL),
				TTL:   uint64(interval.Seconds()), // move to config one day
			}))
		})
	})
})
