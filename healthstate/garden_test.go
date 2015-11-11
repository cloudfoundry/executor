package healthstate_test

import (
	"errors"
	"os"
	"time"

	"github.com/cloudfoundry-incubator/executor"
	gardenFakes "github.com/cloudfoundry-incubator/executor/depot/fakes"
	executorFakes "github.com/cloudfoundry-incubator/executor/fakes"
	. "github.com/cloudfoundry-incubator/executor/healthstate"
	"github.com/pivotal-golang/clock/fakeclock"
	"github.com/pivotal-golang/lager/lagertest"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/ginkgomon"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
)

var _ = Describe("Garden Checker", func() {
	const rootfsPath = "test-rootfs-path"
	var (
		clock             *fakeclock.FakeClock
		duration          time.Duration
		gardenChecker     *GardenChecker
		gardenStoreClient *gardenFakes.FakeGardenStore
		executorClient    *executorFakes.FakeClient
		logger            *lagertest.TestLogger

		signals chan os.Signal
		ready   chan struct{}
		process ifrit.Process
	)

	BeforeEach(func() {
		signals = make(chan os.Signal)
		ready = make(chan struct{})

		duration = 5 * time.Millisecond
		clock = fakeclock.NewFakeClock(time.Unix(123, 456))
		gardenStoreClient = &gardenFakes.FakeGardenStore{}
		executorClient = &executorFakes.FakeClient{}
		logger = lagertest.NewTestLogger("test")
		gardenChecker = New(rootfsPath, executorClient, gardenStoreClient, clock, duration, logger)
	})

	JustBeforeEach(func() {
		process = ginkgomon.Invoke(gardenChecker)
	})

	AfterEach(func() {
		ginkgomon.Kill(process)
	})

	Describe("creating containers", func() {
		It("creates a container and cleans it", func() {
			clock.Increment(duration)
			Eventually(gardenStoreClient.CreateCallCount).Should(Equal(1))
			Eventually(gardenStoreClient.DestroyCallCount).Should(Equal(1))

			_, container := gardenStoreClient.CreateArgsForCall(0)
			Expect(container.Guid).To(ContainSubstring(HealthcheckPrefix))
			Expect(container.RootFSPath).To(Equal(rootfsPath))
			Expect(container.Tags[executor.HealthcheckTag]).To(Equal(executor.HealthcheckTagValue))

			_, containerGuid := gardenStoreClient.DestroyArgsForCall(0)
			Expect(containerGuid).To(Equal(container.Guid))
		})

		Context("when 3 creatins in a row fail", func() {
			BeforeEach(func() {
				gardenStoreClient.CreateReturns(executor.Container{}, errors.New("failed creating container"))
			})

			JustBeforeEach(func() {
				time.Sleep(time.Millisecond)
				clock.Increment(duration)
				time.Sleep(time.Millisecond)
				clock.Increment(duration)
				time.Sleep(time.Millisecond)
				clock.Increment(duration)
				time.Sleep(time.Millisecond)
			})

			It("sets Garden's state to unhealthy", func() {
				Eventually(executorClient.SetHealthyCallCount).Should(Equal(1))
				Consistently(executorClient.SetHealthyCallCount).Should(Equal(1))
				healthy := executorClient.SetHealthyArgsForCall(0)
				Expect(healthy).To(BeFalse())

				Expect(logger).To(gbytes.Say("check-starting"))
				Expect(logger).To(gbytes.Say("check-starting"))
				Expect(logger).To(gbytes.Say("check-starting"))
				Expect(logger).NotTo(gbytes.Say("check-starting"))
			})

			Context("garden starts working agaain", func() {
				JustBeforeEach(func() {
					gardenStoreClient.CreateReturns(executor.Container{}, nil)

					time.Sleep(time.Millisecond)
					clock.Increment(duration)
				})

				It("sets the state to healthy", func() {
					Eventually(executorClient.SetHealthyCallCount).Should(Equal(2))
					healthy := executorClient.SetHealthyArgsForCall(1)
					Expect(healthy).To(BeTrue())
				})
			})
		})
	})

	Context("when we recieve a signal to quit", func() {
		It("tries to stop any running containers", func() {})
	})

})
