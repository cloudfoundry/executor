package log_streamer_factory_test

import (
	"fmt"
	"net"

	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/config"
	. "github.com/onsi/gomega"

	"github.com/cloudfoundry-incubator/executor/log_streamer"
	. "github.com/cloudfoundry-incubator/executor/log_streamer_factory"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

var _ = Describe("LogStreamerFactory", func() {
	var (
		factory   LogStreamerFactory
		logConfig models.LogConfig
	)

	// so we can initialize an emitter :(
	var fakeLoggregatorServer *net.UDPConn

	BeforeEach(func() {
		loggregatorPort := 3456 + config.GinkgoConfig.ParallelNode

		loggregatorServer := fmt.Sprintf("127.0.0.1:%d", loggregatorPort)

		factory = New(loggregatorServer, "conspiracy")

		logConfig = models.LogConfig{}

		var err error

		addr, err := net.ResolveUDPAddr("udp", loggregatorServer)
		Ω(err).ShouldNot(HaveOccurred())

		fakeLoggregatorServer, err = net.ListenUDP("udp", addr)
		Ω(err).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		fakeLoggregatorServer.Close()
	})

	Context("when log config is complete", func() {
		BeforeEach(func() {
			logConfig.SourceName = "fake-source-name"
		})

		It("makes a log streamer", func() {
			Ω(factory(logConfig)).ToNot(BeNil())
		})
	})

	Context("when log config is incomplete", func() {
		BeforeEach(func() {
			logConfig.SourceName = ""
		})

		It("returns a noop streamer", func() {
			Ω(factory(logConfig)).To(Equal(log_streamer.NoopStreamer{}))
		})
	})
})
