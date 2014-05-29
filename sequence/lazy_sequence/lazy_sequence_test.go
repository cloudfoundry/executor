package lazy_sequence_test

import (
	"github.com/cloudfoundry-incubator/executor/sequence"
	"github.com/cloudfoundry-incubator/executor/sequence/fake_step"
	. "github.com/cloudfoundry-incubator/executor/sequence/lazy_sequence"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("LazySequence", func() {
	var lazySequence sequence.Step

	var invokedGenerator bool
	var generatedSteps []sequence.Step

	BeforeEach(func() {
		invokedGenerator = false
		generatedSteps = nil
	})

	JustBeforeEach(func() {
		lazySequence = New(func() []sequence.Step {
			invokedGenerator = true
			return generatedSteps
		})
	})

	Describe("Perform", func() {
		var performed bool

		BeforeEach(func() {
			performed = false

			generatedSteps = []sequence.Step{
				&fake_step.FakeStep{
					PerformStub: func() error {
						performed = true
						return nil
					},
				},
			}
		})

		It("invokes the generator and performs its steps", func() {
			err := lazySequence.Perform()
			Ω(err).ShouldNot(HaveOccurred())

			Ω(invokedGenerator).Should(BeTrue())
			Ω(performed).Should(BeTrue())
		})
	})

	Describe("Cancel", func() {
		var performing chan bool
		var cancelled chan bool

		BeforeEach(func() {
			canceling := make(chan bool)

			performing = make(chan bool)
			cancelled = make(chan bool)

			generatedSteps = []sequence.Step{
				&fake_step.FakeStep{
					PerformStub: func() error {
						performing <- true
						<-canceling
						return nil
					},
					CancelStub: func() {
						canceling <- true
						cancelled <- true
					},
				},
			}
		})

		Context("when the step is running", func() {
			It("cancels it", func() {
				errs := make(chan error)

				go func() {
					errs <- lazySequence.Perform()
				}()

				Eventually(performing).Should(Receive())

				lazySequence.Cancel()

				Eventually(cancelled).Should(Receive())

				var err error
				Eventually(errs).Should(Receive(&err))
				Ω(err).Should(Equal(sequence.CancelledError))
			})
		})

		Context("when the step is not yet running", func() {
			It("prevents it from running", func() {
				lazySequence.Cancel()

				err := lazySequence.Perform()
				Ω(err).Should(Equal(sequence.CancelledError))
			})
		})
	})

	Describe("Cleanup", func() {
		var cleanedUp bool

		BeforeEach(func() {
			cleanedUp = false

			generatedSteps = []sequence.Step{
				&fake_step.FakeStep{
					CleanupStub: func() {
						cleanedUp = true
					},
				},
			}
		})

		Context("when the step has performed", func() {
			It("cleans it up", func() {
				err := lazySequence.Perform()
				Ω(err).ShouldNot(HaveOccurred())

				lazySequence.Cleanup()

				Ω(cleanedUp).Should(BeTrue())
			})
		})

		Context("when the step has not yet performed", func() {
			It("does nothing, successfully", func() {
				lazySequence.Cleanup()
			})
		})
	})
})
