package transformer_test

import (
	"errors"
	"os"
	"time"

	"github.com/cloudfoundry-incubator/bbs/models"
	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/executor/depot/log_streamer"
	"github.com/cloudfoundry-incubator/executor/depot/transformer"
	"github.com/cloudfoundry-incubator/garden"
	"github.com/cloudfoundry/gunk/workpool"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/pivotal-golang/clock/fakeclock"
	"github.com/pivotal-golang/lager"
	"github.com/pivotal-golang/lager/lagertest"

	gfakes "github.com/cloudfoundry-incubator/garden/fakes"
)

var _ = Describe("Transformer", func() {
	Describe("StepsForContainer", func() {
		var (
			logger          lager.Logger
			optimusPrime    transformer.Transformer
			container       executor.Container
			logStreamer     log_streamer.LogStreamer
			gardenContainer *gfakes.FakeContainer
			clock           *fakeclock.FakeClock
		)

		BeforeEach(func() {
			gardenContainer = &gfakes.FakeContainer{}

			logger = lagertest.NewTestLogger("test-container-store")
			logStreamer = log_streamer.New("test", "test", 1)

			healthyMonitoringInterval := 1 * time.Millisecond
			unhealthyMonitoringInterval := 1 * time.Millisecond

			healthCheckWoorkPool, err := workpool.NewWorkPool(1)
			Expect(err).NotTo(HaveOccurred())

			clock = fakeclock.NewFakeClock(time.Now())

			optimusPrime = transformer.NewTransformer(
				nil, nil, nil, nil, nil, nil,
				os.TempDir(),
				false,
				healthyMonitoringInterval,
				unhealthyMonitoringInterval,
				healthCheckWoorkPool,
				clock,
			)

			container = executor.Container{
				GardenContainer: gardenContainer,
				RunInfo: executor.RunInfo{
					Setup: &models.Action{
						RunAction: &models.RunAction{
							Path: "/setup/path",
						},
					},
					Action: &models.Action{
						RunAction: &models.RunAction{
							Path: "/action/path",
						},
					},
					Monitor: &models.Action{
						RunAction: &models.RunAction{
							Path: "/monitor/path",
						},
					},
				},
			}
		})

		Context("when there is no run action", func() {
			BeforeEach(func() {
				container.Action = nil
			})

			It("returns an error", func() {
				_, _, err := optimusPrime.StepsForContainer(logger, container, logStreamer)
				Expect(err).To(HaveOccurred())
			})
		})

		It("returns a step encapsulating setup, monitor, and action", func() {
			setupReceived := make(chan struct{})
			monitorProcess := &gfakes.FakeProcess{}
			gardenContainer.RunStub = func(processSpec garden.ProcessSpec, processIO garden.ProcessIO) (garden.Process, error) {
				if processSpec.Path == "/setup/path" {
					setupReceived <- struct{}{}
				} else if processSpec.Path == "/monitor/path" {
					return monitorProcess, nil
				}
				return &gfakes.FakeProcess{}, nil
			}
			monitorProcess.WaitReturns(1, errors.New("boom"))

			action, hasStartedRunning, err := optimusPrime.StepsForContainer(logger, container, logStreamer)
			Expect(err).NotTo(HaveOccurred())

			errCh := make(chan error)
			go func() {
				errCh <- action.Perform()
			}()

			Eventually(gardenContainer.RunCallCount).Should(Equal(1))
			processSpec, _ := gardenContainer.RunArgsForCall(0)
			Expect(processSpec.Path).To(Equal("/setup/path"))
			Consistently(gardenContainer.RunCallCount).Should(Equal(1))

			<-setupReceived

			Eventually(gardenContainer.RunCallCount).Should(Equal(2))
			processSpec, _ = gardenContainer.RunArgsForCall(1)
			Expect(processSpec.Path).To(Equal("/action/path"))
			Consistently(gardenContainer.RunCallCount).Should(Equal(2))

			Consistently(hasStartedRunning).ShouldNot(Receive())

			clock.Increment(1 * time.Second)
			Eventually(gardenContainer.RunCallCount).Should(Equal(3))
			processSpec, _ = gardenContainer.RunArgsForCall(2)
			Expect(processSpec.Path).To(Equal("/monitor/path"))
			Consistently(hasStartedRunning).ShouldNot(Receive())

			monitorProcess.WaitReturns(0, nil)
			clock.Increment(1 * time.Second)
			Eventually(gardenContainer.RunCallCount).Should(Equal(4))
			processSpec, _ = gardenContainer.RunArgsForCall(3)
			Expect(processSpec.Path).To(Equal("/monitor/path"))
			Eventually(hasStartedRunning).Should(Receive())

			action.Cancel()
			clock.Increment(1 * time.Second)
			Eventually(errCh).Should(Receive(nil))
		})

		Context("when there is no setup", func() {
			BeforeEach(func() {
				container.Setup = nil
			})

			It("returns a codependent step for the action/monitor", func() {
				gardenContainer.RunReturns(&gfakes.FakeProcess{}, nil)

				action, hasStartedRunning, err := optimusPrime.StepsForContainer(logger, container, logStreamer)
				Expect(err).NotTo(HaveOccurred())

				errCh := make(chan error)
				go func() {
					errCh <- action.Perform()
				}()

				Eventually(gardenContainer.RunCallCount).Should(Equal(1))
				processSpec, _ := gardenContainer.RunArgsForCall(0)
				Expect(processSpec.Path).To(Equal("/action/path"))
				Consistently(gardenContainer.RunCallCount).Should(Equal(1))

				clock.Increment(1 * time.Second)
				Eventually(gardenContainer.RunCallCount).Should(Equal(2))
				processSpec, _ = gardenContainer.RunArgsForCall(1)
				Expect(processSpec.Path).To(Equal("/monitor/path"))
				Eventually(hasStartedRunning).Should(Receive())

				action.Cancel()
				clock.Increment(1 * time.Second)
				Eventually(errCh).Should(Receive(nil))
			})
		})

		Context("when there is no monitor", func() {
			BeforeEach(func() {
				container.Monitor = nil
			})

			It("does not run the monitor step and immediately says the healthcheck passed", func() {
				gardenContainer.RunReturns(&gfakes.FakeProcess{}, nil)

				action, hasStartedRunning, err := optimusPrime.StepsForContainer(logger, container, logStreamer)
				Expect(err).NotTo(HaveOccurred())

				errCh := make(chan error)
				go func() {
					errCh <- action.Perform()
				}()

				Expect(hasStartedRunning).To(Receive())

				Eventually(gardenContainer.RunCallCount).Should(Equal(2))
				processSpec, _ := gardenContainer.RunArgsForCall(1)
				Expect(processSpec.Path).To(Equal("/action/path"))
				Consistently(gardenContainer.RunCallCount).Should(Equal(2))
			})
		})
	})
})
