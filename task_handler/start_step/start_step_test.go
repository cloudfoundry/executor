package start_step_test

import (
	"errors"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	steno "github.com/cloudfoundry/gosteno"

	"github.com/cloudfoundry-incubator/executor/sequence"
	. "github.com/cloudfoundry-incubator/executor/task_handler/start_step"
	"github.com/cloudfoundry-incubator/runtime-schema/bbs/fake_bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

var _ = Describe("StartStep", func() {
	var step sequence.Step

	var task models.Task
	var bbs *fake_bbs.FakeExecutorBBS
	var containerHandle string

	BeforeEach(func() {
		task = models.Task{
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
		}

		bbs = fake_bbs.NewFakeExecutorBBS()
		containerHandle = "some-container-handle"

		step = New(
			&task,
			steno.NewLogger("test-logger"),
			bbs,
			&containerHandle,
		)
	})

	Describe("Perform", func() {
		It("starts the Task in the BBS", func() {
			err := step.Perform()
			Ω(err).ShouldNot(HaveOccurred())

			started := bbs.StartedTasks()
			Ω(started).ShouldNot(BeEmpty())
			Ω(started[0].Guid).Should(Equal(task.Guid))
			Ω(started[0].ContainerHandle).Should(Equal(containerHandle))
		})

		Context("when starting the Task in the BBS fails", func() {
			disaster := errors.New("oh no!")

			BeforeEach(func() {
				bbs.SetStartTaskErr(disaster)
			})

			It("sends back the error", func() {
				err := step.Perform()
				Ω(err).Should(Equal(disaster))
			})
		})
	})
})
