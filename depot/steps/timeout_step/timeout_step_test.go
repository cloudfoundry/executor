package timeout_step_test

import (
	"errors"
	"time"

	"github.com/cloudfoundry-incubator/executor/depot/sequence"
	"github.com/cloudfoundry-incubator/executor/depot/sequence/fake_step"
	"github.com/cloudfoundry-incubator/executor/depot/steps/timeout_step"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("TimeoutStep", func() {
	var step sequence.Step
	var subStep1 *fake_step.FakeStep
	var step1PerformError error
	var subStep2 *fake_step.FakeStep

	var thingHappened chan int
	var timeout time.Duration

	BeforeEach(func() {
		thingHappened = make(chan int, 2)

		subStep1 = &fake_step.FakeStep{
			PerformStub: func() error {
				thingHappened <- 1
				return step1PerformError
			},
		}

		subStep2 = &fake_step.FakeStep{
			PerformStub: func() error {
				thingHappened <- 2
				return nil
			},
		}

		timeout = 200 * time.Millisecond
	})

	JustBeforeEach(func() {
		step = timeout_step.New([]sequence.Step{subStep1, subStep2}, timeout)
	})

	It("performs its substeps sequentially", func() {
		err := step.Perform()
		Ω(err).ShouldNot(HaveOccurred())

		Ω(thingHappened).Should(Receive(Equal(1)))
		Ω(thingHappened).Should(Receive(Equal(2)))
	})

	Context("when the first substep returns an error", func() {
		BeforeEach(func() {
			step1PerformError = errors.New("some error")
		})

		It("returns this error", func() {
			err := step.Perform()
			Ω(err).Should(HaveOccurred())
			Ω(err).Should(Equal(step1PerformError))
		})

		It("never performs the second step", func() {
			step.Perform()

			Ω(thingHappened).Should(Receive(Equal(1)))
			Consistently(thingHappened).ShouldNot(Receive())
		})
	})

	Describe("Timeout behaviour", func() {
		Context("When both substeps have time to finish", func() {
			BeforeEach(func() {
				subStep1.PerformStub = func() error {
					time.Sleep(10 * time.Millisecond)
					thingHappened <- 1
					return nil
				}

				subStep2.PerformStub = func() error {
					time.Sleep(10 * time.Millisecond)
					thingHappened <- 2
					return nil
				}
			})

			It("performs both substeps", func() {
				err := step.Perform()
				Ω(err).ShouldNot(HaveOccurred())

				Ω(thingHappened).Should(Receive(Equal(1)))
				Ω(thingHappened).Should(Receive(Equal(2)))
			})
		})

		Context("When only the first substep has time to finish", func() {
			var step2Error = errors.New("step2 error")

			BeforeEach(func() {
				subStep1.PerformStub = func() error {
					time.Sleep(150 * time.Millisecond)
					thingHappened <- 1
					return nil
				}

				subStep2.PerformStub = func() error {
					time.Sleep(150 * time.Millisecond)
					thingHappened <- 2
					return step2Error
				}
			})

			It("returns a timeout error which includes the error returned by step 2", func() {
				err := step.Perform()
				Ω(err).Should(HaveOccurred())
				Ω(err).Should(BeAssignableToTypeOf(timeout_step.TimeoutError{}))
				timeoutErr := err.(timeout_step.TimeoutError)
				Ω(timeoutErr.SubstepError).Should(Equal(step2Error))
			})

			It("cancels the second step", func() {
				step.Perform()

				Ω(subStep1.CancelCallCount()).Should(Equal(0))
				Ω(subStep2.CancelCallCount()).Should(Equal(1))
			})

			It("waits for the second step to complete performing", func() {
				step.Perform()

				Ω(thingHappened).Should(Receive(Equal(1)))
				Ω(thingHappened).Should(Receive(Equal(2)))
			})
		})

		Context("When even the first substep does not have time to finish", func() {
			var step1Error = errors.New("step1 error")

			BeforeEach(func() {
				subStep1.PerformStub = func() error {
					time.Sleep(300 * time.Millisecond)
					thingHappened <- 1
					return step1Error
				}

				subStep2.PerformStub = func() error {
					time.Sleep(300 * time.Millisecond)
					thingHappened <- 2
					return nil
				}
			})

			It("waits until step 1 completes performing", func() {
				step.Perform()

				Ω(thingHappened).Should(Receive(Equal(1)))
				Ω(thingHappened).ShouldNot(Receive())
			})

			It("returns a timeout error which includes the error returned by step 1", func() {
				err := step.Perform()
				Ω(err).Should(HaveOccurred())
				Ω(err).Should(BeAssignableToTypeOf(timeout_step.TimeoutError{}))
				timeoutErr := err.(timeout_step.TimeoutError)
				Ω(timeoutErr.SubstepError).Should(Equal(step1Error))
			})

			It("cancels the first step", func() {
				step.Perform()

				Ω(subStep1.CancelCallCount()).Should(Equal(1))
				Ω(subStep2.CancelCallCount()).Should(Equal(0))
			})

			It("does not even try to perform the second step", func() {
				step.Perform()
				Ω(subStep2.PerformCallCount()).Should(Equal(0))
			})
		})
	})

	Describe("Cancel", func() {
		BeforeEach(func() {
			timeout = 20 * time.Second // the timeout code path should not trigger in these tests
		})

		Context("while step1 is performing", func() {
			var completeStep1 chan struct{}
			var step1PerformStarted chan struct{}
			var step1Error = errors.New("step1 error")
			var performErrChan chan error

			JustBeforeEach(func() {
				completeStep1 = make(chan struct{})
				step1PerformStarted = make(chan struct{})
				performErrChan = make(chan error)

				subStep1.PerformStub = func() error {
					close(step1PerformStarted)
					<-completeStep1
					return step1Error
				}

				go func() {
					performErrChan <- step.Perform()
				}()
				Eventually(step1PerformStarted).Should(BeClosed())
				step.Cancel()
			})

			It("cancels only step1", func() {
				Eventually(subStep1.CancelCallCount).Should(Equal(1))
				close(completeStep1)
				Consistently(subStep2.CancelCallCount).Should(Equal(0))
			})

			It("waits for step1 to complete performing", func() {
				Consistently(performErrChan).ShouldNot(Receive())
				close(completeStep1)
				Eventually(performErrChan).Should(Receive())
			})

			It("returns a cancel error which includes the error returned by step 1", func() {
				close(completeStep1)
				var performErr error
				Eventually(performErrChan).Should(Receive(&performErr))
				Ω(performErr).Should(HaveOccurred())
				Ω(performErr).Should(BeAssignableToTypeOf(timeout_step.CancelError{}))
				cancelErr := performErr.(timeout_step.CancelError)
				Ω(cancelErr.SubstepError).Should(Equal(step1Error))
			})

			It("does not even try to perform the second step", func() {
				close(completeStep1)
				Consistently(subStep2.PerformCallCount()).Should(Equal(0))
			})
		})
	})
})
