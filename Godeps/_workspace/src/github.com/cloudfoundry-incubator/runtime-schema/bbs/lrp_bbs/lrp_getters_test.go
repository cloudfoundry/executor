package lrp_bbs_test

import (
	. "github.com/cloudfoundry-incubator/runtime-schema/bbs/lrp_bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("LrpGetters", func() {
	var bbs *LRPBBS

	BeforeEach(func() {
		bbs = New(etcdClient)
	})

	Describe("GetAllDesiredLRPs", func() {
		lrp1 := models.DesiredLRP{ProcessGuid: "guidA"}
		lrp2 := models.DesiredLRP{ProcessGuid: "guidB"}
		lrp3 := models.DesiredLRP{ProcessGuid: "guidC"}

		BeforeEach(func() {
			err := bbs.DesireLRP(lrp1)
			Ω(err).ShouldNot(HaveOccurred())

			err = bbs.DesireLRP(lrp2)
			Ω(err).ShouldNot(HaveOccurred())

			err = bbs.DesireLRP(lrp3)
			Ω(err).ShouldNot(HaveOccurred())
		})

		It("returns all desired long running processes", func() {
			all, err := bbs.GetAllDesiredLRPs()
			Ω(err).ShouldNot(HaveOccurred())

			Ω(all).Should(Equal([]models.DesiredLRP{lrp1, lrp2, lrp3}))
		})
	})

	Describe("GetDesiredLRPByProcessGuid", func() {
		lrp1 := models.DesiredLRP{ProcessGuid: "guidA"}
		lrp2 := models.DesiredLRP{ProcessGuid: "guidB"}
		lrp3 := models.DesiredLRP{ProcessGuid: "guidC"}

		BeforeEach(func() {
			err := bbs.DesireLRP(lrp1)
			Ω(err).ShouldNot(HaveOccurred())

			err = bbs.DesireLRP(lrp2)
			Ω(err).ShouldNot(HaveOccurred())

			err = bbs.DesireLRP(lrp3)
			Ω(err).ShouldNot(HaveOccurred())
		})

		It("returns all desired long running processes", func() {
			lrp, err := bbs.GetDesiredLRPByProcessGuid("guidA")
			Ω(err).ShouldNot(HaveOccurred())

			Ω(lrp).Should(Equal(lrp1))
		})
	})

	Describe("GetAllActualLRPs", func() {
		lrp1 := models.ActualLRP{ProcessGuid: "guid1", Index: 1, InstanceGuid: "some-instance-guid", State: models.ActualLRPStateRunning}
		lrp2 := models.ActualLRP{ProcessGuid: "guid2", Index: 2, InstanceGuid: "some-instance-guid", State: models.ActualLRPStateStarting}
		lrp3 := models.ActualLRP{ProcessGuid: "guid3", Index: 2, InstanceGuid: "some-instance-guid", State: models.ActualLRPStateRunning}

		BeforeEach(func() {
			err := bbs.ReportActualLRPAsRunning(lrp1)
			Ω(err).ShouldNot(HaveOccurred())

			err = bbs.ReportActualLRPAsStarting(lrp2)
			Ω(err).ShouldNot(HaveOccurred())

			err = bbs.ReportActualLRPAsRunning(lrp3)
			Ω(err).ShouldNot(HaveOccurred())
		})

		It("returns all actual long running processes", func() {
			all, err := bbs.GetAllActualLRPs()
			Ω(err).ShouldNot(HaveOccurred())

			Ω(all).Should(Equal([]models.ActualLRP{lrp1, lrp2, lrp3}))
		})
	})

	Describe("GetRunningActualLRPs", func() {
		lrp1 := models.ActualLRP{ProcessGuid: "guid1", Index: 1, InstanceGuid: "some-instance-guid", State: models.ActualLRPStateRunning}
		lrp2 := models.ActualLRP{ProcessGuid: "guid2", Index: 2, InstanceGuid: "some-instance-guid", State: models.ActualLRPStateStarting}
		lrp3 := models.ActualLRP{ProcessGuid: "guid3", Index: 2, InstanceGuid: "some-instance-guid", State: models.ActualLRPStateRunning}

		BeforeEach(func() {
			err := bbs.ReportActualLRPAsRunning(lrp1)
			Ω(err).ShouldNot(HaveOccurred())

			err = bbs.ReportActualLRPAsStarting(lrp2)
			Ω(err).ShouldNot(HaveOccurred())

			err = bbs.ReportActualLRPAsRunning(lrp3)
			Ω(err).ShouldNot(HaveOccurred())
		})

		It("returns all actual long running processes", func() {
			all, err := bbs.GetRunningActualLRPs()
			Ω(err).ShouldNot(HaveOccurred())

			Ω(all).Should(HaveLen(2))
			Ω(all).Should(ContainElement(lrp1))
			Ω(all).Should(ContainElement(lrp3))
		})
	})

	Describe("GetActualLRPsByProcessGuid", func() {
		lrp1 := models.ActualLRP{ProcessGuid: "guidA", Index: 1, InstanceGuid: "some-instance-guid", State: models.ActualLRPStateRunning}
		lrp2 := models.ActualLRP{ProcessGuid: "guidA", Index: 2, InstanceGuid: "some-instance-guid", State: models.ActualLRPStateStarting}
		lrp3 := models.ActualLRP{ProcessGuid: "guidB", Index: 2, InstanceGuid: "some-instance-guid", State: models.ActualLRPStateRunning}

		BeforeEach(func() {
			err := bbs.ReportActualLRPAsRunning(lrp1)
			Ω(err).ShouldNot(HaveOccurred())

			err = bbs.ReportActualLRPAsStarting(lrp2)
			Ω(err).ShouldNot(HaveOccurred())

			err = bbs.ReportActualLRPAsRunning(lrp3)
			Ω(err).ShouldNot(HaveOccurred())
		})

		It("should fetch all LRPs for the specified guid", func() {
			lrps, err := bbs.GetActualLRPsByProcessGuid("guidA")
			Ω(err).ShouldNot(HaveOccurred())
			Ω(lrps).Should(HaveLen(2))
			Ω(lrps).Should(ContainElement(lrp1))
			Ω(lrps).Should(ContainElement(lrp2))
		})
	})

	Describe("GetRunningActualLRPsByProcessGuid", func() {
		lrp1 := models.ActualLRP{ProcessGuid: "guidA", Index: 1, InstanceGuid: "some-instance-guid", State: models.ActualLRPStateRunning}
		lrp2 := models.ActualLRP{ProcessGuid: "guidA", Index: 2, InstanceGuid: "some-instance-guid", State: models.ActualLRPStateStarting}
		lrp3 := models.ActualLRP{ProcessGuid: "guidB", Index: 2, InstanceGuid: "some-instance-guid", State: models.ActualLRPStateRunning}

		BeforeEach(func() {
			err := bbs.ReportActualLRPAsRunning(lrp1)
			Ω(err).ShouldNot(HaveOccurred())

			err = bbs.ReportActualLRPAsStarting(lrp2)
			Ω(err).ShouldNot(HaveOccurred())

			err = bbs.ReportActualLRPAsRunning(lrp3)
			Ω(err).ShouldNot(HaveOccurred())
		})

		It("should fetch all LRPs for the specified guid", func() {
			lrps, err := bbs.GetRunningActualLRPsByProcessGuid("guidA")
			Ω(err).ShouldNot(HaveOccurred())
			Ω(lrps).Should(HaveLen(1))
			Ω(lrps).Should(ContainElement(lrp1))
		})
	})
})
