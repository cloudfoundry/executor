package bbs_test

import (
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	. "github.com/cloudfoundry-incubator/runtime-schema/bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/gunk/timeprovider/faketimeprovider"
)

var _ = Describe("RunOnce BBS", func() {
	var bbs *BBS
	var runOnce *models.RunOnce
	var timeProvider *faketimeprovider.FakeTimeProvider

	BeforeEach(func() {
		timeProvider = faketimeprovider.New(time.Unix(1238, 0))
		bbs = New(store, timeProvider)
		runOnce = &models.RunOnce{
			Guid:      "some-guid",
			CreatedAt: time.Now().UnixNano(),
		}
	})

	Describe("GetAllPendingRunOnces", func() {
		BeforeEach(func() {
			err := bbs.DesireRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())
		})

		It("returns all RunOnces in 'pending' state", func() {
			runOnces, err := bbs.GetAllPendingRunOnces()
			Ω(err).ShouldNot(HaveOccurred())
			Ω(runOnces).Should(HaveLen(1))
			Ω(runOnces).Should(ContainElement(runOnce))
		})
	})

	Describe("GetAllClaimedRunOnces", func() {
		BeforeEach(func() {
			err := bbs.DesireRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())

			err = bbs.ClaimRunOnce(runOnce, "executor-ID")
			Ω(err).ShouldNot(HaveOccurred())
		})

		It("returns all RunOnces in 'claimed' state", func() {
			runOnces, err := bbs.GetAllClaimedRunOnces()
			Ω(err).ShouldNot(HaveOccurred())
			Ω(runOnces).Should(HaveLen(1))
			Ω(runOnces).Should(ContainElement(runOnce))
		})
	})

	Describe("GetAllStartingRunOnces", func() {
		BeforeEach(func() {
			err := bbs.DesireRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())

			err = bbs.ClaimRunOnce(runOnce, "executor-ID")
			Ω(err).ShouldNot(HaveOccurred())

			err = bbs.StartRunOnce(runOnce, "container-handle")
			Ω(err).ShouldNot(HaveOccurred())
		})

		It("returns all RunOnces in 'running' state", func() {
			runOnces, err := bbs.GetAllStartingRunOnces()
			Ω(err).ShouldNot(HaveOccurred())
			Ω(runOnces).Should(HaveLen(1))
			Ω(runOnces).Should(ContainElement(runOnce))
		})
	})

	Describe("GetAllCompletedRunOnces", func() {
		BeforeEach(func() {
			err := bbs.DesireRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())

			err = bbs.ClaimRunOnce(runOnce, "executor-ID")
			Ω(err).ShouldNot(HaveOccurred())

			err = bbs.StartRunOnce(runOnce, "container-handle")
			Ω(err).ShouldNot(HaveOccurred())

			err = bbs.CompleteRunOnce(runOnce, true, "a reason", "a result")
			Ω(err).ShouldNot(HaveOccurred())
		})

		It("returns all RunOnces in 'completed' state", func() {
			runOnces, err := bbs.GetAllCompletedRunOnces()
			Ω(err).ShouldNot(HaveOccurred())
			Ω(runOnces).Should(HaveLen(1))
			Ω(runOnces).Should(ContainElement(runOnce))
		})
	})
})
