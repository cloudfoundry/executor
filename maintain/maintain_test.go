package maintain_test

import (
	"os"
	"syscall"
	"time"

	"github.com/cloudfoundry-incubator/executor/maintain"
	"github.com/cloudfoundry-incubator/runtime-schema/bbs/fake_bbs"
	steno "github.com/cloudfoundry/gosteno"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Maintain Presence", func() {
	var id = "executor-id"
	var heartbeatInterval = 1 * time.Second

	var fakeBBS *fake_bbs.FakeExecutorBBS
	var sigChan chan os.Signal
	var logger *steno.Logger

	var maintainer *maintain.Maintainer

	BeforeSuite(func() {
		steno.EnterTestMode(steno.LOG_DEBUG)
	})

	BeforeEach(func() {
		fakeBBS = fake_bbs.NewFakeExecutorBBS()
		logger = steno.NewLogger("test-logger")
		sigChan = make(chan os.Signal, 1)
		maintainer = maintain.New(id, fakeBBS, logger, heartbeatInterval)
	})

	AfterEach(func() {
		sigChan <- syscall.SIGTERM
	})

	Context("when maintaining presence", func() {
		BeforeEach(func() {
			go func() {
				err := maintainer.Run(sigChan, nil)
				Ω(err).ShouldNot(HaveOccurred())
			}()
		})
		It("should maintain presence", func() {
			Eventually(fakeBBS.GetMaintainExecutorPresenceId).Should(Equal("executor-id"))
		})
	})

	Context("when we fail to maintain our presence", func() {
		var err error

		BeforeEach(func() {
			fakeBBS.MaintainExecutorPresenceOutputs.Presence = &fake_bbs.FakePresence{
				MaintainStatus: false,
			}

			errChan := make(chan error, 1)
			runToEnd := func() bool {
				errChan <- maintainer.Run(sigChan, nil)
				return true
			}

			Eventually(runToEnd, 1).Should(BeTrue())
			err = <-errChan
		})

		It("sends an error to the given error channel", func() {
			Ω(err).ShouldNot(BeNil())
		})

		It("logs an error message", func() {
			testSink := steno.GetMeTheGlobalTestSink()

			records := []*steno.Record{}

			lockMessageIndex := 0
			Eventually(func() string {
				records = testSink.Records()

				if len(records) > 0 {
					lockMessageIndex := len(records) - 1
					return records[lockMessageIndex].Message
				}

				return ""
			}, 1.0, 0.1).Should(Equal("executor.maintain_presence.failed"))

			Ω(records[lockMessageIndex].Level).Should(Equal(steno.LOG_ERROR))
		})
	})
})
