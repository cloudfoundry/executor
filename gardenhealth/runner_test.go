package gardenhealth_test

import (
	"time"

	"github.com/cloudfoundry-incubator/executor/gardenhealth"

	fakeexecutor "github.com/cloudfoundry-incubator/executor/fakes"
	"github.com/cloudfoundry-incubator/executor/gardenhealth/fakegardenhealth"
	"github.com/pivotal-golang/clock/fakeclock"
	"github.com/pivotal-golang/lager/lagertest"

	. "github.com/onsi/ginkgo"
	//	. "github.com/onsi/gomega"
)

var _ = Describe("Runner", func() {
	const interval = 10 * time.Second
	var runner *gardenhealth.Runner
	var logger *lagertest.TestLogger
	var checker *fakegardenhealth.FakeChecker
	var executorClient *fakeexecutor.FakeClient
	var clock *fakeclock.FakeClock

	BeforeEach(func() {
		logger = lagertest.NewTestLogger("test")
		checker = &fakegardenhealth.FakeChecker{}
		executorClient = &fakeexecutor.FakeClient{}
		clock = &fakeclock.FakeClock{}
		runner = gardenhealth.NewRunner(interval, logger, checker, executorClient, clock)
	})

	Describe("Run", func() {
		Context("When garden is healthy", func() {
			It("Sets healthy to true only once", func() {})
			It("Continues to check at the correct interval", func() {})
		})
		Context("When garden is unhealthy", func() {
			It("Sets healthy to false after three failed checks", func() {})
		})

		Context("When garden is intermittently healthy", func() {
			It("Sets healthy to false, then to true", func() {})
		})

		Context("When garden has an unrecoverable error", func() {
			It("exits with an error", func() {})
		})

		Context("When the runner is signaled", func() {
			It("exits imediately with no error", func() {})
		})
	})
})
