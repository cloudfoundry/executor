package checks_test

import (
	. "github.com/cloudfoundry-incubator/executor/checks"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
)

var _ = Describe("Dial", func() {
	var (
		server *ghttp.Server

		dial Check
	)

	BeforeEach(func() {
		server = ghttp.NewServer()
		dial = NewDial("tcp", server.HTTPTestServer.Listener.Addr().String())
	})

	Context("when dialing fails", func() {
		BeforeEach(func() {
			server.Close()
		})

		It("returns false", func() {
			Ω(dial.Check()).Should(BeFalse())
		})
	})

	Context("when the endpoint is listening", func() {
		It("returns true", func() {
			Ω(dial.Check()).Should(BeTrue())
		})
	})
})
