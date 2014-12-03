package steps_test

import (
	"errors"
	"time"

	"github.com/cloudfoundry-incubator/executor/depot/steps"
	"github.com/cloudfoundry-incubator/executor/depot/steps/fakes"
	"github.com/pivotal-golang/lager/lagertest"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("TimeoutStep", func() {
	var (
		substepReadyChan    chan struct{}
		substepPerformTime  time.Duration
		substepFinishedChan chan struct{}
		substepPerformError error
		substep             *fakes.FakeStep

		timeout time.Duration
		logger  *lagertest.TestLogger
	)

	BeforeEach(func() {
		substepReadyChan = make(chan struct{})
		substepFinishedChan = make(chan struct{})

		substep = &fakes.FakeStep{
			PerformStub: func() error {
				close(substepReadyChan)
				time.Sleep(substepPerformTime)
				close(substepFinishedChan)
				return substepPerformError
			},
		}

		logger = lagertest.NewTestLogger("test")
	})

	Describe("Perform", func() {
		var err error

		JustBeforeEach(func() {
			err = steps.NewTimeout(substep, timeout, logger).Perform()
		})

		Context("When the substep finishes before the timeout expires", func() {
			BeforeEach(func() {
				substepPerformTime = 10 * time.Millisecond
				timeout = 100 * time.Millisecond
			})

			Context("when the substep returns an error", func() {
				BeforeEach(func() {
					substepPerformError = errors.New("some error")
				})

				It("performs the substep", func() {
					Ω(substepFinishedChan).Should(BeClosed())
				})

				It("returns this error", func() {
					Ω(err).Should(HaveOccurred())
					Ω(err).Should(Equal(substepPerformError))
				})
			})

			Context("when the substep does not error", func() {
				BeforeEach(func() {
					substepPerformError = nil
				})

				It("performs the substep", func() {
					Ω(substepFinishedChan).Should(BeClosed())
				})

				It("does not error", func() {
					Ω(err).ShouldNot(HaveOccurred())
				})
			})
		})

		Context("When the timeout expires before the substep finishes", func() {
			BeforeEach(func() {
				substepPerformTime = 100 * time.Millisecond
				timeout = 10 * time.Millisecond
			})

			It("cancels the substep", func() {
				Ω(substep.CancelCallCount()).Should(Equal(1))
			})

			It("waits until the substep completes performing", func() {
				Ω(substepFinishedChan).Should(BeClosed())
			})

			It("logs the timeout", func() {
				Eventually(logger.TestSink.LogMessages).Should(ConsistOf([]string{
					"test.TimeoutAction.timed-out",
				}))
			})

			Context("when the substep does not error", func() {
				BeforeEach(func() {
					substepPerformError = nil
				})

				It("returns a timeout error", func() {
					Ω(err).Should(HaveOccurred())
					Ω(err).Should(BeAssignableToTypeOf(steps.TimeoutError{}))
				})
			})

			Context("when the substep returns an error", func() {
				BeforeEach(func() {
					substepPerformError = errors.New("some error")
				})

				It("returns a timeout error which includes the error returned by the substep", func() {
					Ω(err).Should(HaveOccurred())
					Ω(err).Should(BeAssignableToTypeOf(steps.TimeoutError{}))
					timeoutErr := err.(steps.TimeoutError)
					Ω(timeoutErr.SubstepError).Should(Equal(substepPerformError))
				})
			})
		})
	})

	Describe("Cancel", func() {
		BeforeEach(func() {
			timeout = 20 * time.Second // the timeout code path should not trigger in these tests
		})

		var durationBeforeCancel time.Duration
		var err error

		JustBeforeEach(func() {
			step := steps.NewTimeout(substep, timeout, logger)
			errChan := make(chan error, 1)

			go func() {
				defer GinkgoRecover()
				errChan <- step.Perform()
			}()

			time.Sleep(durationBeforeCancel)
			step.Cancel()
			err = <-errChan
		})

		Context("When the substep finishes before the step is cancelled", func() {
			BeforeEach(func() {
				substepPerformTime = 10 * time.Millisecond
				durationBeforeCancel = 100 * time.Millisecond
			})

			Context("when the substep returns an error", func() {
				BeforeEach(func() {
					substepPerformError = errors.New("some error")
				})

				It("performs the substep", func() {
					Ω(substepFinishedChan).Should(BeClosed())
				})

				It("returns this error", func() {
					Ω(err).Should(Equal(substepPerformError))
				})
			})

			Context("when the substep does not error", func() {
				BeforeEach(func() {
					substepPerformError = nil
				})

				It("performs the substep", func() {
					Ω(substepFinishedChan).Should(BeClosed())
				})

				It("does not error", func() {
					Ω(err).ShouldNot(HaveOccurred())
				})
			})
		})

		Context("When the step is cancelled before the substep finishes", func() {
			BeforeEach(func() {
				substepPerformTime = 100 * time.Millisecond
				durationBeforeCancel = 10 * time.Millisecond
			})

			It("cancels the substep", func() {
				Ω(substep.CancelCallCount()).Should(Equal(1))
			})

			It("waits until the substep completes performing", func() {
				Ω(substepFinishedChan).Should(BeClosed())
			})

			It("logs the cancellation", func() {
				Eventually(logger.TestSink.LogMessages).Should(ConsistOf([]string{
					"test.TimeoutAction.cancelling",
				}))
			})

			Context("when the substep does not error", func() {
				BeforeEach(func() {
					substepPerformError = nil
				})

				It("returns a cancel error", func() {
					Ω(err).Should(BeAssignableToTypeOf(steps.CancelError{}))
				})
			})

			Context("when the substep returns an error", func() {
				BeforeEach(func() {
					substepPerformError = errors.New("some error")
				})

				It("returns a cancel error which includes the error returned by the substep", func() {
					Ω(err).Should(BeAssignableToTypeOf(steps.CancelError{}))
					timeoutErr := err.(steps.CancelError)
					Ω(timeoutErr.SubstepError).Should(Equal(substepPerformError))
				})
			})
		})
	})
})
