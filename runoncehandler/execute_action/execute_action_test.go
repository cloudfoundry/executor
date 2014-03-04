package execute_action_test

import (
	"errors"
	"fmt"
	"net"

	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/config"
	. "github.com/onsi/gomega"

	"github.com/cloudfoundry-incubator/runtime-schema/bbs/fakebbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"

	"github.com/cloudfoundry-incubator/executor/actionrunner/fakeactionrunner"
	. "github.com/cloudfoundry-incubator/executor/runoncehandler/execute_action"
)

var _ = Describe("ExecuteAction", func() {
	var action *ExecuteAction
	var result chan error

	var runOnce models.RunOnce
	var bbs *fakebbs.FakeExecutorBBS
	var actionRunner *fakeactionrunner.FakeActionRunner // TODO: this may go away
	var loggregatorServer string
	var loggregatorSecret string

	// so we can initialize an emitter :(
	var fakeLoggregatorServer *net.UDPConn

	BeforeEach(func() {
		result = make(chan error)

		runOnce = models.RunOnce{
			Guid:  "totally-unique",
			Stack: "penguin",
			Actions: []models.ExecutorAction{
				{
					models.RunAction{
						Script: "sudo reboot",
					},
				},
			},

			ExecutorID: "some-executor-id",

			ContainerHandle: "some-container-handle",
		}

		bbs = fakebbs.NewFakeExecutorBBS()

		actionRunner = fakeactionrunner.New()

		loggregatorPort := 3456 + config.GinkgoConfig.ParallelNode
		loggregatorServer = fmt.Sprintf("127.0.0.1:%d", loggregatorPort)
		loggregatorSecret = "conspiracy"

		action = New(
			&runOnce,
			steno.NewLogger("test-logger"),
			bbs,
			actionRunner,
			loggregatorServer,
			loggregatorSecret,
		)
	})

	Describe("Perform", func() {
		It("starts the RunOnce in the BBS", func() {
			go action.Perform(result)
			Ω(<-result).Should(BeNil())

			Ω(bbs.StartedRunOnce.Guid).Should(Equal(runOnce.Guid))
		})

		It("starts running the actions", func() {
			go action.Perform(result)
			Ω(<-result).Should(BeNil())

			Ω(actionRunner.ContainerHandle).Should(Equal(runOnce.ContainerHandle))
			Ω(actionRunner.Actions).Should(Equal(runOnce.Actions))
		})

		It("does not initialize with the streamer by default", func() {
			go action.Perform(result)
			Ω(<-result).Should(BeNil())

			Ω(actionRunner.Streamer).Should(BeNil())
		})

		Context("when logs are configured on the RunOnce", func() {
			BeforeEach(func() {
				runOnceWithLog := runOnce

				runOnceWithLog.Log = models.LogConfig{
					Guid:       "totally-unique",
					SourceName: "XYZ",
					Index:      nil,
				}

				runOnce = runOnceWithLog

				var err error

				addr, err := net.ResolveUDPAddr("udp", loggregatorServer)
				Ω(err).ShouldNot(HaveOccurred())

				fakeLoggregatorServer, err = net.ListenUDP("udp", addr)
				Ω(err).ShouldNot(HaveOccurred())
			})

			AfterEach(func() {
				fakeLoggregatorServer.Close()
			})

			It("runs the actions with a streamer", func() {
				go action.Perform(result)
				Ω(<-result).Should(BeNil())

				Ω(actionRunner.Streamer).ShouldNot(BeNil())
			})
		})

		Context("when the RunOnce actions succeed", func() {
			BeforeEach(func() {
				actionRunner.RunResult = "runonce-result"
			})

			It("sets the Result on the RunOnce", func() {
				Ω(runOnce.Result).Should(BeZero())

				go action.Perform(result)
				Ω(<-result).Should(BeNil())

				Ω(runOnce.Result).Should(Equal("runonce-result"))
			})
		})

		Context("when the RunOnce actions fail", func() {
			disaster := errors.New("oh no!")

			BeforeEach(func() {
				actionRunner.RunError = disaster
			})

			It("sets Failed to true on the RunOnce with the error as the reason", func() {
				Ω(runOnce.Result).Should(BeZero())

				go action.Perform(result)
				Ω(<-result).Should(BeNil())

				Ω(runOnce.Failed).Should(BeTrue())
				Ω(runOnce.FailureReason).Should(Equal("oh no!"))
			})
		})

		Context("when starting the RunOnce in the BBS fails", func() {
			disaster := errors.New("oh no!")

			BeforeEach(func() {
				bbs.StartRunOnceErr = disaster
			})

			It("sends back the error", func() {
				go action.Perform(result)
				Ω(<-result).Should(Equal(disaster))
			})
		})
	})
})
