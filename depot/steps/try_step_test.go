package steps_test

import (
	"errors"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/pivotal-golang/lager/lagertest"

	. "github.com/cloudfoundry-incubator/executor/depot/steps"
	"github.com/cloudfoundry-incubator/executor/depot/steps/fakes"
)

var _ = Describe("TryStep", func() {
	var step Step
	var subStep Step
	var thingHappened bool
	var cancelled bool
	var logger *lagertest.TestLogger

	BeforeEach(func() {
		thingHappened, cancelled = false, false

		subStep = &fakes.FakeStep{
			PerformStub: func() error {
				thingHappened = true
				return nil
			},
			CancelStub: func() {
				cancelled = true
			},
		}

		logger = lagertest.NewTestLogger("test")
	})

	JustBeforeEach(func() {
		step = NewTry(subStep, logger)
	})

	It("performs its substep", func() {
		err := step.Perform()
		Ω(err).ShouldNot(HaveOccurred())

		Ω(thingHappened).To(BeTrue())
	})

	Context("when the substep fails", func() {
		disaster := errors.New("oh no!")

		BeforeEach(func() {
			subStep = &fakes.FakeStep{
				PerformStub: func() error {
					return disaster
				},
			}
		})

		It("succeeds anyway", func() {
			err := step.Perform()
			Ω(err).ShouldNot(HaveOccurred())
		})

		It("logs the failure", func() {
			err := step.Perform()
			Ω(err).ShouldNot(HaveOccurred())

			Ω(logger).Should(gbytes.Say("failed"))
			Ω(logger).Should(gbytes.Say("oh no!"))
		})
	})

	Context("when told to cancel", func() {
		It("passes the message along", func() {
			Ω(cancelled).Should(BeFalse())
			step.Cancel()
			Ω(cancelled).Should(BeTrue())
		})
	})
})
