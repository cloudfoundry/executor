package try_step_test

import (
	"errors"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/cloudfoundry-incubator/executor/sequence"
	"github.com/cloudfoundry-incubator/executor/sequence/fake_step"
	. "github.com/cloudfoundry-incubator/executor/steps/try_step"
	steno "github.com/cloudfoundry/gosteno"
)

var _ = Describe("TryStep", func() {
	var step sequence.Step
	var subStep sequence.Step
	var thingHappened bool
	var cleanedUp bool
	var cancelled bool
	var fakeLogger *steno.Logger

	BeforeEach(func() {
		thingHappened, cleanedUp, cancelled = false, false, false

		steno.EnterTestMode(steno.LOG_DEBUG)

		subStep = fake_step.FakeStep{
			WhenPerforming: func() error {
				thingHappened = true
				return nil
			},
			WhenCleaningUp: func() {
				cleanedUp = true
			},
			WhenCancelling: func() {
				cancelled = true
			},
		}

		fakeLogger = steno.NewLogger("test-logger")
	})

	JustBeforeEach(func() {
		step = New(subStep, fakeLogger)
	})

	It("performs its substep", func() {
		err := step.Perform()
		Ω(err).ShouldNot(HaveOccurred())

		Ω(thingHappened).To(BeTrue())
	})

	Context("when the substep fails", func() {
		disaster := errors.New("oh no!")

		BeforeEach(func() {
			subStep = fake_step.FakeStep{
				WhenPerforming: func() error {
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

			testSink := steno.GetMeTheGlobalTestSink()

			records := testSink.Records()
			Ω(records).ShouldNot(BeEmpty())

			lastRecord := records[len(records)-1]

			Ω(lastRecord.Message).Should(Equal("try.failed"))
			Ω(lastRecord.Data["error"]).Should(Equal("oh no!"))
			Ω(lastRecord.Level).Should(Equal(steno.LOG_WARN))
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
