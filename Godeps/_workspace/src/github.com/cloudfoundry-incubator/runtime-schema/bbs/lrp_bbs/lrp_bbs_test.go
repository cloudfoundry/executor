package lrp_bbs_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	. "github.com/cloudfoundry-incubator/runtime-schema/bbs/lrp_bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

var _ = Describe("LRP", func() {
	var bbs *LongRunningProcessBBS

	BeforeEach(func() {
		bbs = New(etcdClient)
	})

	Describe("DesireLongRunningProcess", func() {
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
			err := bbs.DesireLongRunningProcess(lrp)
			Ω(err).ShouldNot(HaveOccurred())

			node, err := etcdClient.Get("/v1/desired/some-process-guid")
			Ω(err).ShouldNot(HaveOccurred())

			Ω(node.Value).Should(Equal(lrp.ToJSON()))
		})

		Context("when the store is out of commission", func() {
			itRetriesUntilStoreComesBack(func() error {
				return bbs.DesireLongRunningProcess(lrp)
			})
		})
	})

	Describe("ReportLongRunningProcessAsRunning", func() {
		var lrp models.LRP

		BeforeEach(func() {
			lrp = models.LRP{
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

		It("creates /v1/actual/<process-guid>/<index>/<instance-guid>", func() {
			err := bbs.ReportLongRunningProcessAsRunning(lrp)
			Ω(err).ShouldNot(HaveOccurred())

			node, err := etcdClient.Get("/v1/actual/some-process-guid/1/some-instance-guid")
			Ω(err).ShouldNot(HaveOccurred())

			Ω(node.Value).Should(Equal(lrp.ToJSON()))
		})

		Context("when the store is out of commission", func() {
			itRetriesUntilStoreComesBack(func() error {
				return bbs.ReportLongRunningProcessAsRunning(lrp)
			})
		})
	})
})
