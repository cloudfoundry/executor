package lrp_bbs_test

import (
	"github.com/cloudfoundry-incubator/runtime-schema/bbs/shared"
	"github.com/cloudfoundry-incubator/runtime-schema/models"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("LrpWatchers", func() {
	Describe("WatchForDesiredLRPChanges", func() {
		var (
			events <-chan models.DesiredLRPChange
			stop   chan<- bool
			errors <-chan error
		)

		lrp := models.DesiredLRP{
			ProcessGuid: "some-process-guid",
			Instances:   5,
			Stack:       "some-stack",
			MemoryMB:    1024,
			DiskMB:      512,
			Routes:      []string{"route-1", "route-2"},
		}

		BeforeEach(func() {
			events, stop, errors = bbs.WatchForDesiredLRPChanges()
		})

		AfterEach(func() {
			stop <- true
		})

		It("sends an event down the pipe for creates", func() {
			err := bbs.DesireLRP(lrp)
			Ω(err).ShouldNot(HaveOccurred())

			Eventually(events).Should(Receive(Equal(models.DesiredLRPChange{
				Before: nil,
				After:  &lrp,
			})))
		})

		It("sends an event down the pipe for updates", func() {
			err := bbs.DesireLRP(lrp)
			Ω(err).ShouldNot(HaveOccurred())

			Eventually(events).Should(Receive())

			changedLRP := lrp
			changedLRP.Instances++

			err = bbs.DesireLRP(changedLRP)
			Ω(err).ShouldNot(HaveOccurred())

			Eventually(events).Should(Receive(Equal(models.DesiredLRPChange{
				Before: &lrp,
				After:  &changedLRP,
			})))
		})

		It("sends an event down the pipe for deletes", func() {
			err := bbs.DesireLRP(lrp)
			Ω(err).ShouldNot(HaveOccurred())

			Eventually(events).Should(Receive())

			err = etcdClient.Delete(shared.DesiredLRPSchemaPath(lrp))
			Ω(err).ShouldNot(HaveOccurred())

			Eventually(events).Should(Receive(Equal(models.DesiredLRPChange{
				Before: &lrp,
				After:  nil,
			})))
		})
	})

	Describe("WatchForActualLRPChanges", func() {
		var (
			events <-chan models.ActualLRPChange
			stop   chan<- bool
			errors <-chan error
			lrp    models.ActualLRP
		)

		BeforeEach(func() {
			lrp = models.ActualLRP{ProcessGuid: "some-process-guid", State: models.ActualLRPStateStarting, Since: timeProvider.Time().UnixNano(), ExecutorID: "executor-id"}
			events, stop, errors = bbs.WatchForActualLRPChanges()
		})

		AfterEach(func() {
			stop <- true
		})

		It("sends an event down the pipe for creates", func() {
			err := bbs.ReportActualLRPAsStarting(lrp, "executor-id")
			Ω(err).ShouldNot(HaveOccurred())

			Eventually(events).Should(Receive(Equal(models.ActualLRPChange{
				Before: nil,
				After:  &lrp,
			})))
		})

		It("sends an event down the pipe for updates", func() {
			err := bbs.ReportActualLRPAsStarting(lrp, "executor-id")
			Ω(err).ShouldNot(HaveOccurred())

			Eventually(events).Should(Receive())

			changedLRP := lrp
			changedLRP.State = models.ActualLRPStateRunning
			changedLRP.ExecutorID = "executor-id"

			err = bbs.ReportActualLRPAsRunning(changedLRP, "executor-id")
			Ω(err).ShouldNot(HaveOccurred())

			Eventually(events).Should(Receive(Equal(models.ActualLRPChange{
				Before: &lrp,
				After:  &changedLRP,
			})))
		})

		It("sends an event down the pipe for delete", func() {
			err := bbs.ReportActualLRPAsStarting(lrp, "executor-id")
			Ω(err).ShouldNot(HaveOccurred())

			Eventually(events).Should(Receive())

			err = bbs.RemoveActualLRP(lrp)
			Ω(err).ShouldNot(HaveOccurred())

			Eventually(events).Should(Receive(Equal(models.ActualLRPChange{
				Before: &lrp,
				After:  nil,
			})))
		})
	})

})
