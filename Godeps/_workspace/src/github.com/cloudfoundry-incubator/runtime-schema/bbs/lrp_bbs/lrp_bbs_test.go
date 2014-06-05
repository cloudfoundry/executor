package lrp_bbs_test

import (
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/storeadapter"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("LRP", func() {
	executorID := "some-executor-id"

	Describe("Adding and removing DesireLRP", func() {
		var lrp models.DesiredLRP

		BeforeEach(func() {
			lrp = models.DesiredLRP{
				ProcessGuid: "some-process-guid",
				Instances:   5,
				Stack:       "some-stack",
				MemoryMB:    1024,
				DiskMB:      512,
				Routes:      []string{"route-1", "route-2"},
			}
		})

		It("creates /v1/desired/<process-guid>/<index>", func() {
			err := bbs.DesireLRP(lrp)
			Ω(err).ShouldNot(HaveOccurred())

			node, err := etcdClient.Get("/v1/desired/some-process-guid")
			Ω(err).ShouldNot(HaveOccurred())
			Ω(node.Value).Should(Equal(lrp.ToJSON()))
		})

		Context("when deleting the DesiredLRP", func() {
			BeforeEach(func() {
				err := bbs.DesireLRP(lrp)
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("should delete it", func() {
				err := bbs.RemoveDesiredLRPByProcessGuid(lrp.ProcessGuid)
				Ω(err).ShouldNot(HaveOccurred())

				_, err = etcdClient.Get("/v1/desired/some-process-guid")
				Ω(err).Should(MatchError(storeadapter.ErrorKeyNotFound))
			})

			Context("when the desired LRP does not exist", func() {
				It("should not error", func() {
					err := bbs.RemoveDesiredLRPByProcessGuid("monkey")
					Ω(err).ShouldNot(HaveOccurred())
				})
			})
		})

		Context("when the store is out of commission", func() {
			itRetriesUntilStoreComesBack(func() error {
				return bbs.DesireLRP(lrp)
			})
		})
	})

	Describe("Adding and removing actual LRPs", func() {
		var lrp models.ActualLRP

		BeforeEach(func() {
			lrp = models.ActualLRP{
				ProcessGuid:  "some-process-guid",
				InstanceGuid: "some-instance-guid",
				Index:        1,

				Host: "1.2.3.4",
				Ports: []models.PortMapping{
					{ContainerPort: 8080, HostPort: 65100},
					{ContainerPort: 8081, HostPort: 65101},
				},
			}
		})

		Describe("ReportActualLRPAsStarting", func() {
			It("creates /v1/actual/<process-guid>/<index>/<instance-guid>", func() {
				err := bbs.ReportActualLRPAsStarting(lrp, executorID)
				Ω(err).ShouldNot(HaveOccurred())

				node, err := etcdClient.Get("/v1/actual/some-process-guid/1/some-instance-guid")
				Ω(err).ShouldNot(HaveOccurred())

				expectedLRP := lrp
				expectedLRP.State = models.ActualLRPStateStarting
				expectedLRP.Since = timeProvider.Time().UnixNano()
				expectedLRP.ExecutorID = executorID
				Ω(node.Value).Should(MatchJSON(expectedLRP.ToJSON()))
			})

			Context("when the store is out of commission", func() {
				itRetriesUntilStoreComesBack(func() error {
					return bbs.ReportActualLRPAsStarting(lrp, executorID)
				})
			})
		})

		Describe("ReportActualLRPAsRunning", func() {
			It("creates /v1/actual/<process-guid>/<index>/<instance-guid>", func() {
				err := bbs.ReportActualLRPAsRunning(lrp, executorID)
				Ω(err).ShouldNot(HaveOccurred())

				node, err := etcdClient.Get("/v1/actual/some-process-guid/1/some-instance-guid")
				Ω(err).ShouldNot(HaveOccurred())

				expectedLRP := lrp
				expectedLRP.State = models.ActualLRPStateRunning
				expectedLRP.Since = timeProvider.Time().UnixNano()
				expectedLRP.ExecutorID = executorID
				Ω(node.Value).Should(MatchJSON(expectedLRP.ToJSON()))
			})

			Context("when the store is out of commission", func() {
				itRetriesUntilStoreComesBack(func() error {
					return bbs.ReportActualLRPAsRunning(lrp, executorID)
				})
			})
		})

		Describe("RemoveActualLRP", func() {
			BeforeEach(func() {
				bbs.ReportActualLRPAsStarting(lrp, executorID)
			})

			It("should remove the LRP", func() {
				err := bbs.RemoveActualLRP(lrp)
				Ω(err).ShouldNot(HaveOccurred())

				_, err = etcdClient.Get("/v1/actual/some-process-guid/1/some-instance-guid")
				Ω(err).Should(MatchError(storeadapter.ErrorKeyNotFound))
			})

			Context("when the store is out of commission", func() {
				itRetriesUntilStoreComesBack(func() error {
					return bbs.RemoveActualLRP(lrp)
				})
			})
		})
	})

})
