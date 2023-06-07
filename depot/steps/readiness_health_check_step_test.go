package steps_test

import (
	"errors"
	"os"

	"code.cloudfoundry.org/executor/depot/log_streamer/fake_log_streamer"
	"code.cloudfoundry.org/executor/depot/steps"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/tedsuo/ifrit"
	fake_runner "github.com/tedsuo/ifrit/fake_runner_v2"
)

var _ = Describe("NewReadinessHealthCheckStep", func() {
	var (
		fakeStreamer                *fake_log_streamer.FakeLogStreamer
		untilReadyCheck             *fake_runner.TestRunner
		untilFailureCheck           *fake_runner.TestRunner
		process                     ifrit.Process
		needToKillUntilFailureCheck bool
		needToKillUntilReadyCheck   bool
		readinessChan               chan int

		step ifrit.Runner
	)

	BeforeEach(func() {
		fakeStreamer = newFakeStreamer()
		untilReadyCheck = fake_runner.NewTestRunner()
		untilFailureCheck = fake_runner.NewTestRunner()
		needToKillUntilFailureCheck = true
		needToKillUntilReadyCheck = true
		readinessChan = make(chan int, 2)
	})

	JustBeforeEach(func() {
		step = steps.NewReadinessHealthCheckStep(
			untilReadyCheck,
			untilFailureCheck,
			fakeStreamer,
			readinessChan,
		)
		process = ifrit.Background(step)
	})

	AfterEach(func() {
		if needToKillUntilReadyCheck == true {
			Eventually(untilReadyCheck.RunCallCount).Should(Equal(1))
			untilReadyCheck.TriggerExit(errors.New("booom!"))
		}
		untilReadyCheck.EnsureExit()

		if needToKillUntilFailureCheck == true {
			Eventually(untilFailureCheck.RunCallCount).Should(Equal(1))
			untilFailureCheck.TriggerExit(errors.New("booom!"))
		}
		untilFailureCheck.EnsureExit()
	})

	Describe("Run", func() {
		BeforeEach(func() {
			needToKillUntilReadyCheck = true
			needToKillUntilFailureCheck = false
		})

		It("emits a message to the applications log stream", func() {
			Eventually(fakeStreamer.Stdout().(*gbytes.Buffer)).Should(
				gbytes.Say("Starting readiness health monitoring of container\n"),
			)
		})

		It("Runs the untilReady check", func() {
			Eventually(untilReadyCheck.RunCallCount).Should(Equal(1))
		})

		Context("the untilReady check succeeds", func() {
			BeforeEach(func() {
				needToKillUntilReadyCheck = false
				needToKillUntilFailureCheck = true
			})

			JustBeforeEach(func() {
				Consistently(fakeStreamer.Stdout().(*gbytes.Buffer)).ShouldNot(
					gbytes.Say("App is ready!\n"),
				)

				untilReadyCheck.TriggerExit(nil)
			})

			It("emits a message to the application log stream", func() {
				Eventually(fakeStreamer.Stdout().(*gbytes.Buffer)).Should(
					gbytes.Say("App is ready!\n"),
				)
			})

			It("becomes ready", func() {
				Eventually(process.Ready()).Should(BeClosed())
			})

			It("runs the untilFailure check", func() {
				Eventually(untilFailureCheck.RunCallCount).Should(Equal(1))
			})

			It("writes ready to the readiness channel", func() {
				Eventually(readinessChan).Should(Receive(Equal(1)))

			})

			Context("when the untilFailure check exits with an error", func() {
				JustBeforeEach(func() {
					Eventually(untilFailureCheck.RunCallCount).Should(Equal(1))
					untilFailureCheck.TriggerExit(errors.New("crash"))
					needToKillUntilFailureCheck = false
					needToKillUntilReadyCheck = false
				})

				It("emits a message to the application log stream", func() {
					Eventually(fakeStreamer.Stdout().(*gbytes.Buffer)).Should(
						gbytes.Say("Oh no! The app is not ready anymore\n"),
					)
				})

				It("writes notReady to the readiness channel", func() {
					Eventually(readinessChan).Should(Receive(Equal(2)))
				})

				XIt("the step exits with nil", func() {
					var err error
					Eventually(process.Wait()).Should(Receive(&err))
					Expect(err).To(MatchError(nil))
				})
			})
		})

		Context("the untilReady check fails to run", func() {
			var disaster error
			BeforeEach(func() {
				needToKillUntilFailureCheck = false
				needToKillUntilReadyCheck = false
			})

			JustBeforeEach(func() {
				disaster = errors.New("booom!")
				untilReadyCheck.TriggerExit(disaster)
			})

			It("the step exits with an error", func() {
				var err error
				Eventually(process.Wait()).Should(Receive(&err))
				Expect(err).To(MatchError(ContainSubstring("booom!")))
			})

			It("does not run the untilFailure check", func() {
				Consistently(untilFailureCheck.RunCallCount).Should(Equal(0))
			})

			It("emits a message to the application log stream", func() {
				Eventually(fakeStreamer.Stdout().(*gbytes.Buffer)).Should(
					gbytes.Say("Failed to run the untilReady check\n"),
				)
			})

			It("does not become ready", func() {
				Consistently(process.Ready()).ShouldNot(BeClosed())
			})
		})
	})

	Describe("Signalling", func() {
		BeforeEach(func() {
			needToKillUntilReadyCheck = false
			needToKillUntilFailureCheck = false
		})

		Context("while doing untilReady check", func() {
			JustBeforeEach(func() {
				Eventually(untilReadyCheck.RunCallCount).Should(Equal(1))
				process.Signal(os.Interrupt)
				Eventually(untilReadyCheck.WaitForCall()).Should(Receive(Equal(os.Interrupt)))
				untilReadyCheck.TriggerExit(nil)
			})

			It("cancels the in-flight check", func() {
				Eventually(process.Wait()).Should(Receive(Equal(new(steps.CancelledError))))
			})

			It("does not run the untilFailure check", func() {
				Consistently(untilFailureCheck.RunCallCount).Should(Equal(0))
			})
		})

		Context("while doing the  check", func() {
			JustBeforeEach(func() {
				untilReadyCheck.TriggerExit(nil)
				Eventually(untilFailureCheck.RunCallCount).Should(Equal(1))
			})

			It("cancels the in-flight check", func() {
				process.Signal(os.Interrupt)
				Eventually(untilFailureCheck.WaitForCall()).Should(Receive(Equal(os.Interrupt)))
				untilFailureCheck.TriggerExit(nil)
				Eventually(process.Wait()).Should(Receive(Equal(new(steps.CancelledError))))
			})
		})
	})
})
