package steps_test

import (
	"errors"
	"os"

	"github.com/hashicorp/go-multierror"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/fake_runner"

	"code.cloudfoundry.org/executor/depot/steps"
)

var _ = Describe("CodependentStep", func() {
	var (
		step     ifrit.Runner
		subStep1 *fake_runner.FakeRunner
		subStep2 *fake_runner.FakeRunner

		subStep1TriggerExit func(err error)
		subStep2TriggerExit func(err error)

		subStep1Cancelled chan struct{}
		subStep2Cancelled chan struct{}

		triggerReady1 chan struct{}
		triggerReady2 chan struct{}

		exitChan1, exitChan2 chan error

		process ifrit.Process
	)

	var errorOnExit bool
	var cancelOthersOnExit bool

	BeforeEach(func() {
		errorOnExit = false
		cancelOthersOnExit = false

		subStep1Cancelled = make(chan struct{})
		subStep2Cancelled = make(chan struct{})

		triggerReady1 = make(chan struct{})
		triggerReady2 = make(chan struct{})

		subStep1 = &fake_runner.FakeRunner{}
		subStep2 = &fake_runner.FakeRunner{}
		exitChan1 = make(chan error, 1)
		exitChan2 = make(chan error, 1)
		subStep1TriggerExit = func(err error) { exitChan1 <- err }
		subStep1.RunStub = func(signals <-chan os.Signal, ready chan<- struct{}) error {
			select {
			case <-triggerReady1:
				close(ready)
			case <-signals:
				close(subStep1Cancelled)
				return steps.ErrCancelled
			case err := <-exitChan1:
				return err
			}
			select {
			case <-signals:
				close(subStep1Cancelled)
				return steps.ErrCancelled
			case err := <-exitChan1:
				return err
			}
		}

		subStep2TriggerExit = func(err error) { exitChan2 <- err }
		subStep2.RunStub = func(signals <-chan os.Signal, ready chan<- struct{}) error {
			select {
			case <-triggerReady2:
				close(ready)
			case <-signals:
				close(subStep2Cancelled)
				return steps.ErrCancelled
			case err := <-exitChan2:
				return err
			}
			select {
			case <-signals:
				close(subStep2Cancelled)
				return steps.ErrCancelled
			case err := <-exitChan2:
				return err
			}
		}
	})

	AfterEach(func() {
		select {
		case exitChan1 <- nil:
		default:
		}
		select {
		case exitChan2 <- nil:
		default:
		}
	})

	JustBeforeEach(func() {
		step = steps.NewCodependent([]ifrit.Runner{subStep1, subStep2}, errorOnExit, cancelOthersOnExit)
		process = ifrit.Background(step)
		Eventually(subStep1.RunCallCount).Should(Equal(1))
		Eventually(subStep2.RunCallCount).Should(Equal(1))
	})

	Describe("Run", func() {
		It("runs its substeps in parallel", func() {
			subStep1TriggerExit(nil)
			subStep2TriggerExit(nil)

			Eventually(process.Wait()).Should(Receive(BeNil()))
		})

		Context("when one of the substeps fails", func() {
			disaster := errors.New("oh no!")

			BeforeEach(func() {
				subStep1TriggerExit(disaster)
			})

			It("returns an aggregate of the failures", func() {
				var err *multierror.Error
				Eventually(process.Wait()).Should(Receive(&err))
				Expect(err.WrappedErrors()).To(ConsistOf(disaster))
			})

			It("cancels all remaining steps", func() {
				Eventually(subStep2Cancelled).Should(BeClosed())
			})
		})
		Context("when step is cancelled", func() {
			disaster := errors.New("oh no!")

			BeforeEach(func() {
				subStep1TriggerExit(disaster)
				subStep2TriggerExit(steps.ErrCancelled)
			})

			It("does not add cancelled error to message", func() {
				var err *multierror.Error
				Eventually(process.Wait()).Should(Receive(&err))
				Expect(err.Error()).To(Equal("oh no!"))
			})
		})

		Context("when one of the substeps exits without failure", func() {
			It("continues to run the other step", func() {
				subStep1TriggerExit(nil)
				Consistently(process.Wait()).ShouldNot(Receive())
			})

			Context("when cancelOthersOnExit is set to true", func() {
				BeforeEach(func() {
					cancelOthersOnExit = true
				})

				It("should cancel all other steps", func() {
					subStep1TriggerExit(nil)
					Eventually(subStep2Cancelled).Should(BeClosed())
					Eventually(process.Wait()).Should(Receive())
				})
			})

			Context("when errorOnExit is set to true", func() {
				BeforeEach(func() {
					errorOnExit = true
				})

				It("returns an aggregate of the failures", func() {
					subStep1TriggerExit(nil)
					var err *multierror.Error
					Eventually(process.Wait()).Should(Receive(&err))
					Expect(err.WrappedErrors()).To(ConsistOf(steps.CodependentStepExitedError))
				})

				It("should cancel all other steps", func() {
					subStep1TriggerExit(nil)
					Eventually(subStep2Cancelled).Should(BeClosed())
					Eventually(process.Wait()).Should(Receive())
				})

				It("should cancel all other steps regardless of the step that failed", func() {
					subStep2TriggerExit(nil)
					Eventually(subStep1Cancelled).Should(BeClosed())
					Eventually(process.Wait()).Should(Receive())
				})
			})
		})

		Context("when multiple substeps fail", func() {
			disaster1 := errors.New("oh no")
			disaster2 := errors.New("oh my")

			BeforeEach(func() {
				// ensure subSteps cannot be cancelled when the other step fail
				subStep2.RunStub = func(signals <-chan os.Signal, ready chan<- struct{}) error {
					return <-exitChan2
				}
				subStep1.RunStub = func(signals <-chan os.Signal, ready chan<- struct{}) error {
					return <-exitChan1
				}
				subStep1TriggerExit(disaster1)
				subStep2TriggerExit(disaster2)
			})

			It("joins the error messages with a semicolon", func() {
				var err *multierror.Error
				Eventually(process.Wait()).Should(Receive(&err))
				errMsg := err.Error()
				Expect(errMsg).NotTo(HavePrefix(";"))
				Expect(errMsg).To(ContainSubstring("oh no"))
				Expect(errMsg).To(ContainSubstring("oh my"))
				Expect(errMsg).To(MatchRegexp(`\w+; \w+`))
			})
		})
	})

	Describe("Ready", func() {
		It("becomes ready only when all substeps are ready", func() {
			Consistently(process.Ready()).ShouldNot(BeClosed())
			close(triggerReady1)
			Consistently(process.Ready()).ShouldNot(BeClosed())
			close(triggerReady2)
			Eventually(process.Ready()).Should(BeClosed())
		})
	})

	Describe("Signal", func() {
		It("cancels all sub-steps", func() {
			Consistently(process.Wait()).ShouldNot(Receive())

			process.Signal(os.Interrupt)

			Eventually(subStep1Cancelled).Should(BeClosed())
			Eventually(subStep2Cancelled).Should(BeClosed())

			Eventually(process.Wait()).Should(Receive())
		})
	})
})
