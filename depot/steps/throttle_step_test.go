package steps_test

import (
	"code.cloudfoundry.org/executor/depot/steps"
	"code.cloudfoundry.org/executor/depot/steps/fakes"
	"code.cloudfoundry.org/workpool"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("ThrottleStep", func() {
	var (
		step         steps.Step
		throttleChan chan struct{}
		doneChan     chan struct{}
		fakeStep     *fakes.FakeStep
	)

	const numConcurrentSteps = 3

	BeforeEach(func() {
		throttleChan = make(chan struct{}, numConcurrentSteps)
		doneChan = make(chan struct{}, 1)
		fakeStep = new(fakes.FakeStep)
		fakeStep.PerformStub = func() error {
			throttleChan <- struct{}{}
			<-doneChan
			return nil
		}

	})

	JustBeforeEach(func() {
		workPool, err := workpool.NewWorkPool(numConcurrentSteps)
		Expect(err).NotTo(HaveOccurred())
		step = steps.NewThrottle(fakeStep, workPool)
	})

	AfterEach(func() {
		step.Cancel()
	})

	It("throttles its substep", func() {
		for i := 0; i < 5; i++ {
			go step.Perform()
		}

		Eventually(func() int {
			return len(throttleChan)
		}).Should(Equal(numConcurrentSteps))
		Consistently(func() int {
			return len(throttleChan)
		}).Should(Equal(numConcurrentSteps))

		Eventually(fakeStep.PerformCallCount).Should(Equal(numConcurrentSteps))

		doneChan <- struct{}{}

		Eventually(fakeStep.PerformCallCount).Should(Equal(numConcurrentSteps + 1))

		close(doneChan)

		Eventually(fakeStep.PerformCallCount).Should(Equal(5))
	})
})
