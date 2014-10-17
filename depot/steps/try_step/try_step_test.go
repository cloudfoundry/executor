package try_step_test

import (
	"errors"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/pivotal-golang/lager/lagertest"

	"github.com/cloudfoundry-incubator/executor/depot/sequence"
	"github.com/cloudfoundry-incubator/executor/depot/sequence/fake_step"
	. "github.com/cloudfoundry-incubator/executor/depot/steps/try_step"
)

var _ = Describe("TryStep", func() {
	var step sequence.Step
	var subStep sequence.Step
	var thingHappened bool
	var cleanedUp bool
	var cancelled bool
	var logger *lagertest.TestLogger

	BeforeEach(func() {
		thingHappened, cleanedUp, cancelled = false, false, false

		subStep = &fake_step.FakeStep{
			PerformStub: func() error {
				thingHappened = true
				return nil
			},
			CleanupStub: func() {
				cleanedUp = true
			},
			CancelStub: func() {
				cancelled = true
			},
		}

		logger = lagertest.NewTestLogger("test")
	})

	JustBeforeEach(func() {
		step = New(subStep, logger)
	})

	It("performs its substep", func() {
		err := step.Perform()
		Ω(err).ShouldNot(HaveOccurred())

		Ω(thingHappened).To(BeTrue())
	})

	Context("when the substep fails", func() {
		disaster := errors.New("oh no!")

		BeforeEach(func() {
			subStep = &fake_step.FakeStep{
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

			Ω(logger.TestSink.Buffer).Should(gbytes.Say("failed"))
			Ω(logger.TestSink.Buffer).Should(gbytes.Say("oh no!"))
		})
	})

	Context("when told to clean up", func() {
		It("passes the message along", func() {
			Ω(cleanedUp).Should(BeFalse())
			step.Cleanup()
			Ω(cleanedUp).Should(BeTrue())
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
