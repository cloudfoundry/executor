package steps_test

import (
	"errors"
	"time"

	"code.cloudfoundry.org/clock/fakeclock"
	"code.cloudfoundry.org/executor/depot/steps"
	"code.cloudfoundry.org/executor/depot/steps/fakes"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("ConsistentlySucceedsStep", func() {
	var (
		step      steps.Step
		fakeStep  *fakes.FakeStep
		fakeClock *fakeclock.FakeClock
		errCh     chan error
		blockCh   chan error
		done      chan struct{}
	)

	AfterEach(func() {
		close(blockCh)
	})

	BeforeEach(func() {
		errCh = make(chan error, 1)
		done = make(chan struct{}, 1)
		fakeClock = fakeclock.NewFakeClock(time.Now())
		fakeStep = &fakes.FakeStep{}
		blockCh = make(chan error, 10)
		fakeStep.PerformStub = func() error {
			return <-blockCh
		}

		step = steps.NewConsistentlySucceedsStep(func() steps.Step { return fakeStep }, time.Second, fakeClock)
	})

	JustBeforeEach(func() {
		go func() {
			errCh <- step.Perform()
			close(done)
		}()
	})

	AfterEach(func() {
		step.Cancel()
		Eventually(done).Should(BeClosed())
	})

	It("should not exit", func() {
		Consistently(errCh).ShouldNot(Receive(BeNil()))
	})

	It("does not perform the substep initially", func() {
		Consistently(fakeStep.PerformCallCount).Should(BeZero())
	})

	Context("when the step is cancelled", func() {
		JustBeforeEach(func() {
			step.Cancel()
		})

		It("cancels the substep", func() {
			Eventually(errCh).Should(Receive(MatchError(steps.ErrCancelled)))
		})
	})

	Context("when the step is stuck", func() {
		Context("and the step is cancelled", func() {
			JustBeforeEach(func() {
				fakeClock.WaitForWatcherAndIncrement(time.Second)
				Eventually(fakeStep.PerformCallCount).Should(Equal(1))
				step.Cancel()
			})

			It("cancels the substep", func() {
				Eventually(fakeStep.CancelCallCount).ShouldNot(BeZero())
				blockCh <- errors.New("BOOOM")
				Eventually(errCh).Should(Receive(MatchError("BOOOM")))
			})
		})
	})

	Context("when the step is succeeding", func() {
		JustBeforeEach(func() {
			fakeClock.WaitForWatcherAndIncrement(time.Second)
			Eventually(fakeStep.PerformCallCount).Should(Equal(1))
			blockCh <- nil
		})

		It("retries after the timeout has elapsed", func() {
			fakeClock.WaitForWatcherAndIncrement(time.Second)
			Eventually(fakeStep.PerformCallCount).Should(Equal(2))
		})

		Context("when it later fails", func() {
			JustBeforeEach(func() {
				fakeClock.WaitForWatcherAndIncrement(time.Second)
				Eventually(fakeStep.PerformCallCount).Should(Equal(2))
				blockCh <- errors.New("BOOOM")
			})

			It("should fail", func() {
				Eventually(errCh).Should(Receive(MatchError("BOOOM")))
			})
		})
	})
})
