package steps_test

import (
	"errors"
	"os"
	"time"

	"code.cloudfoundry.org/clock/fakeclock"
	"code.cloudfoundry.org/executor/depot/steps"
	"code.cloudfoundry.org/executor/depot/steps/fakes"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/tedsuo/ifrit"
)

var _ = FDescribe("EventuallySucceedsStep", func() {
	var (
		step    ifrit.Runner
		process ifrit.Process

		fakeStep  *fakes.FakeStep
		fakeClock *fakeclock.FakeClock
		blockCh   chan error
	)

	BeforeEach(func() {
		fakeClock = fakeclock.NewFakeClock(time.Now())
		fakeStep = &fakes.FakeStep{}
		blockCh = make(chan error, 10)
		fakeStep.PerformStub = func() error {
			return <-blockCh
		}

		step = steps.NewEventuallySucceedsStep(func() steps.Step { return fakeStep }, time.Second, 10*time.Second, fakeClock)
	})

	JustBeforeEach(func() {
		process = ifrit.Background(step)
	})

	AfterEach(func() {
		close(blockCh)
		process.Signal(os.Interrupt)
		Eventually(process.Wait()).Should(Receive())
	})

	It("should not trigger the substep initially", func() {
		Consistently(fakeStep.PerformCallCount).Should(BeZero())
	})

	Context("when the step succeeds", func() {
		JustBeforeEach(func() {
			fakeClock.WaitForWatcherAndIncrement(time.Second)
			blockCh <- nil
		})

		It("should exits with no errors", func() {
			Eventually(process.Wait()).Should(Receive(BeNil()))
		})
	})

	Context("when the step is stuck", func() {
		Context("and the step is signalled", func() {
			JustBeforeEach(func() {
				fakeClock.WaitForWatcherAndIncrement(time.Second)
				Eventually(fakeStep.PerformCallCount).Should(Equal(1))
				process.Signal(os.Interrupt)
			})

			It("cancels the substep", func() {
				Eventually(fakeStep.CancelCallCount).ShouldNot(BeZero())
				blockCh <- errors.New("BOOOOM")
				Eventually(process.Wait()).Should(Receive(MatchError("BOOOOM")))
			})
		})
	})

	Context("when the step is failing", func() {
		JustBeforeEach(func() {
			fakeClock.WaitForWatcherAndIncrement(time.Second)
			blockCh <- errors.New("BOOOOM")
		})

		Context("when the step is cancelled", func() {
			JustBeforeEach(func() {
				process.Signal(os.Interrupt)
			})

			It("returns ErrCancelled", func() {
				Eventually(process.Wait()).Should(Receive(MatchError(steps.ErrCancelled)))
			})
		})

		It("retries after the timeout has elapsed", func() {
			fakeClock.WaitForWatcherAndIncrement(time.Second)
			Eventually(fakeStep.PerformCallCount).Should(Equal(2))
		})

		Context("and the timeout elapsed", func() {
			JustBeforeEach(func() {
				Eventually(fakeStep.PerformCallCount).Should(Equal(1))
				fakeClock.WaitForWatcherAndIncrement(10 * time.Second)
				blockCh <- errors.New("BOOOOM")
			})

			It("returns the last error received from the substep", func() {
				Eventually(process.Wait()).Should(Receive(MatchError(ContainSubstring("BOOOOM"))))
			})
		})

		Context("when it later succeed", func() {
			JustBeforeEach(func() {
				Eventually(fakeStep.PerformCallCount).Should(Equal(1))
				fakeClock.WaitForWatcherAndIncrement(time.Second)
				blockCh <- nil
			})

			It("should succeed", func() {
				Eventually(process.Wait()).Should(Receive(BeNil()))
			})
		})
	})
})
