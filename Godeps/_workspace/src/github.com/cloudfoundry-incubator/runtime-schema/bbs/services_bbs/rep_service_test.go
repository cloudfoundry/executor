package services_bbs_test

import (
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	. "github.com/cloudfoundry-incubator/runtime-schema/bbs/services_bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/bbs/shared"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/cloudfoundry/storeadapter"
	"github.com/cloudfoundry/storeadapter/test_helpers"
)

var _ = Describe("Fetching all Reps", func() {
	var (
		bbs               *ServicesBBS
		interval          time.Duration
		status            <-chan bool
		err               error
		firstPresence     Presence
		secondPresence    Presence
		firstRepPresence  models.RepPresence
		secondRepPresence models.RepPresence
	)

	BeforeEach(func() {
		logSink := steno.NewTestingSink()

		steno.Init(&steno.Config{
			Sinks: []steno.Sink{logSink},
		})

		logger := steno.NewLogger("the-logger")
		steno.EnterTestMode()

		bbs = New(etcdClient, logger)

		firstRepPresence = models.RepPresence{
			RepID: "first-rep",
			Stack: "lucid64",
		}

		secondRepPresence = models.RepPresence{
			RepID: "second-rep",
			Stack: ".Net",
		}
	})

	Describe("GetAllReps", func() {
		Context("when there are available Reps", func() {
			BeforeEach(func() {
				interval = 1 * time.Second

				firstPresence, status, err = bbs.MaintainRepPresence(interval, firstRepPresence)
				Ω(err).ShouldNot(HaveOccurred())
				Eventually(test_helpers.NewStatusReporter(status).Locked).Should(BeTrue())

				secondPresence, status, err = bbs.MaintainRepPresence(interval, secondRepPresence)
				Ω(err).ShouldNot(HaveOccurred())
				Eventually(test_helpers.NewStatusReporter(status).Locked).Should(BeTrue())
			})

			AfterEach(func() {
				firstPresence.Remove()
				secondPresence.Remove()
			})

			It("should get from /v1/rep/", func() {
				repPresences, err := bbs.GetAllReps()
				Ω(err).ShouldNot(HaveOccurred())
				Ω(repPresences).Should(HaveLen(2))
				Ω(repPresences).Should(ContainElement(firstRepPresence))
				Ω(repPresences).Should(ContainElement(secondRepPresence))
			})

			Context("when there is unparsable JSON in there...", func() {
				BeforeEach(func() {
					etcdClient.Create(storeadapter.StoreNode{
						Key:   shared.RepSchemaPath("blah"),
						Value: []byte("ß"),
					})
				})

				It("should ignore the unparsable JSON and move on", func() {
					repPresences, err := bbs.GetAllReps()
					Ω(err).ShouldNot(HaveOccurred())
					Ω(repPresences).Should(HaveLen(2))
					Ω(repPresences).Should(ContainElement(firstRepPresence))
					Ω(repPresences).Should(ContainElement(secondRepPresence))
				})
			})
		})

		Context("when there are none", func() {
			It("should return empty", func() {
				reps, err := bbs.GetAllReps()
				Ω(err).ShouldNot(HaveOccurred())
				Ω(reps).Should(BeEmpty())
			})
		})
	})
})
