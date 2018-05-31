package steps_test

import (
	"errors"
	"os"

	"code.cloudfoundry.org/executor/depot/steps"
	"code.cloudfoundry.org/workpool"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/fake_runner"
)

var _ = Describe("ThrottleStep", func() {
	const numConcurrentSteps = 3

	var (
		step     ifrit.Runner
		fakeStep *fake_runner.TestRunner
	)

	BeforeEach(func() {
		fakeStep = fake_runner.NewTestRunner()
		workPool, err := workpool.NewWorkPool(numConcurrentSteps)
		Expect(err).NotTo(HaveOccurred())
		step = steps.NewThrottle(fakeStep, workPool)
	})

	Describe("Run", func() {
		var (
			throttleChan   chan struct{}
			doneChan       chan struct{}
			substepExitErr = errors.New("substp exit")
		)

		BeforeEach(func() {
			throttleChan = make(chan struct{}, numConcurrentSteps)
			doneChan = make(chan struct{}, 1)

			fakeStep.RunStub = func(signals <-chan os.Signal, ready chan<- struct{}) error {
				throttleChan <- struct{}{}
				<-doneChan
				return substepExitErr
			}
		})

		It("throttles its substep", func() {
			for i := 0; i < 5; i++ {
				go step.Run(nil, nil)
			}

			Eventually(func() int {
				return len(throttleChan)
			}).Should(Equal(numConcurrentSteps))

			Consistently(func() int {
				return len(throttleChan)
			}).Should(Equal(numConcurrentSteps))

			Eventually(fakeStep.RunCallCount).Should(Equal(numConcurrentSteps))

			doneChan <- struct{}{}

			Eventually(fakeStep.RunCallCount).Should(Equal(numConcurrentSteps + 1))

			close(doneChan)

			Eventually(fakeStep.RunCallCount).Should(Equal(5))
		})

		It("becomes ready once the substep is ready", func() {
			process := ifrit.Background(step)
			Consistently(process.Ready()).ShouldNot(BeClosed())
			fakeStep.TriggerReady()
			Eventually(process.Ready()).Should(BeClosed())
		})

		It("exits with the result of the substep", func() {
			process := ifrit.Background(step)
			Eventually(fakeStep.RunCallCount).Should(Equal(1))
			Consistently(process.Wait()).ShouldNot(Receive())
			doneChan <- struct{}{}
			Eventually(process.Wait()).Should(Receive(MatchError(substepExitErr)))
		})
	})

	Describe("Signalling", func() {
		It("signals the throttled substep", func() {
			process := ifrit.Background(step)
			Eventually(fakeStep.RunCallCount).Should(Equal(1))
			Consistently(fakeStep.WaitForCall()).ShouldNot(Receive())
			process.Signal(os.Interrupt)
			Eventually(fakeStep.WaitForCall()).Should(Receive())
		})
	})
})
