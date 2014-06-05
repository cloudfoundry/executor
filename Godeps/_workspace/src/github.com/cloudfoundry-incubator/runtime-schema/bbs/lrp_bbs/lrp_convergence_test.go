package lrp_bbs_test

import (
	"github.com/cloudfoundry-incubator/runtime-schema/bbs/shared"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/storeadapter"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("LrpConvergence", func() {
	var executorID string
	processGuid := "process-guid"

	BeforeEach(func() {
		executorID = "the-executor-id"
		etcdClient.Create(storeadapter.StoreNode{
			Key:   shared.ExecutorSchemaPath(executorID),
			Value: []byte{},
		})

		actualLrp1 := models.ActualLRP{
			ProcessGuid:  processGuid,
			InstanceGuid: "instance-guid-1",
			Index:        0,
		}
		bbs.ReportActualLRPAsStarting(actualLrp1, executorID)

		actualLrp2 := models.ActualLRP{
			ProcessGuid:  processGuid,
			InstanceGuid: "instance-guid-2",
			Index:        1,
		}
		bbs.ReportActualLRPAsStarting(actualLrp2, executorID)
	})

	Context("when no executor is missing", func() {
		It("should not prune any LRPs", func() {
			bbs.ConvergeLRPs()
			Ω(bbs.GetAllActualLRPs()).Should(HaveLen(2))
		})
	})

	Context("when an executor is missing", func() {
		BeforeEach(func() {
			etcdClient.Delete(shared.ExecutorSchemaPath(executorID))
		})

		It("should delete LRPs associated with said executor", func() {
			bbs.ConvergeLRPs()
			Ω(bbs.GetAllActualLRPs()).Should(BeEmpty())
		})
	})

	Describe("when there is a desired LRP", func() {
		var desiredEvents <-chan models.DesiredLRPChange
		var desiredLRP models.DesiredLRP

		commenceWatching := func() {
			desiredEvents, _, _ = bbs.WatchForDesiredLRPChanges()
		}

		BeforeEach(func() {
			desiredLRP = models.DesiredLRP{
				ProcessGuid: processGuid,
				Instances:   2,
			}
		})

		Context("when the desired LRP has malformed JSON", func() {
			BeforeEach(func() {
				err := etcdClient.SetMulti([]storeadapter.StoreNode{
					{
						Key:   shared.DesiredLRPSchemaPathByProcessGuid("bogus-desired"),
						Value: []byte("ß"),
					},
				})

				Ω(err).ShouldNot(HaveOccurred())
			})

			It("should delete the bogus entry", func() {
				bbs.ConvergeLRPs()
				_, err := etcdClient.Get(shared.DesiredLRPSchemaPathByProcessGuid("bogus-desired"))
				Ω(err).Should(MatchError(storeadapter.ErrorKeyNotFound))
			})
		})

		Context("when the desired LRP has all its actual LRPs, and there are no extras", func() {
			BeforeEach(func() {
				bbs.DesireLRP(desiredLRP)
			})

			It("should not kick the desired LRP", func() {
				commenceWatching()
				bbs.ConvergeLRPs()

				Consistently(desiredEvents).ShouldNot(Receive())
			})
		})

		Context("when the desired LRP is missing actuals", func() {
			BeforeEach(func() {
				desiredLRP.Instances = 3
				bbs.DesireLRP(desiredLRP)
			})

			It("should kick the desired LRP", func() {
				commenceWatching()
				bbs.ConvergeLRPs()

				var noticedOnce models.DesiredLRPChange
				Eventually(desiredEvents).Should(Receive(&noticedOnce))
				Ω(*noticedOnce.After).Should(Equal(desiredLRP))
			})
		})

		Context("when there are extra actual LRPs", func() {
			BeforeEach(func() {
				desiredLRP.Instances = 1
				bbs.DesireLRP(desiredLRP)
			})

			It("should kick the desired LRP", func() {
				commenceWatching()
				bbs.ConvergeLRPs()

				var noticedOnce models.DesiredLRPChange
				Eventually(desiredEvents).Should(Receive(&noticedOnce))
				Ω(*noticedOnce.After).Should(Equal(desiredLRP))
			})
		})

		Context("when there are duplicate actual LRPs", func() {
			BeforeEach(func() {
				actualLrp := models.ActualLRP{
					ProcessGuid:  processGuid,
					InstanceGuid: "instance-guid-duplicate",
					Index:        0,
				}

				bbs.ReportActualLRPAsStarting(actualLrp, executorID)
				bbs.DesireLRP(desiredLRP)
			})

			It("should kick the desired LRP", func() {
				commenceWatching()
				bbs.ConvergeLRPs()

				var noticedOnce models.DesiredLRPChange
				Eventually(desiredEvents).Should(Receive(&noticedOnce))
				Ω(*noticedOnce.After).Should(Equal(desiredLRP))
			})
		})
	})

	Context("when there is an actual LRP with no matching desired LRP", func() {
		It("should emit a stop for the actual LRP", func() {
			bbs.ConvergeLRPs()
			stops, err := bbs.GetAllStopLRPInstances()
			Ω(err).ShouldNot(HaveOccurred())
			Ω(stops).Should(HaveLen(2))

			Ω(stops).Should(ContainElement(models.StopLRPInstance{
				ProcessGuid:  processGuid,
				InstanceGuid: "instance-guid-1",
				Index:        0,
			}))

			Ω(stops).Should(ContainElement(models.StopLRPInstance{
				ProcessGuid:  processGuid,
				InstanceGuid: "instance-guid-2",
				Index:        1,
			}))
		})
	})
})
