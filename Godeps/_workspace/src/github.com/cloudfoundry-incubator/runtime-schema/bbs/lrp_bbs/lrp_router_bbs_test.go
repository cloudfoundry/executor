package lrp_bbs_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	. "github.com/cloudfoundry-incubator/runtime-schema/bbs/lrp_bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

var _ = Describe("LRPRouter", func() {
	var bbs *LongRunningProcessBBS

	BeforeEach(func() {
		bbs = New(etcdClient)
	})

	Describe("WatchForDesiredLongRunningProcesses", func() {
		var (
			events  <-chan models.DesiredLRP
			stop    chan<- bool
			errors  <-chan error
			stopped bool
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
			events, stop, errors = bbs.WatchForDesiredLongRunningProcesses()
		})

		AfterEach(func() {
			if !stopped {
				stop <- true
			}
		})

		It("sends an event down the pipe for creates", func() {
			err := bbs.DesireLongRunningProcess(lrp)
			Ω(err).ShouldNot(HaveOccurred())

			Eventually(events).Should(Receive(Equal(lrp)))
		})

		It("sends an event down the pipe for updates", func() {
			err := bbs.DesireLongRunningProcess(lrp)
			Ω(err).ShouldNot(HaveOccurred())

			Eventually(events).Should(Receive(Equal(lrp)))

			err = bbs.DesireLongRunningProcess(lrp)
			Ω(err).ShouldNot(HaveOccurred())

			Eventually(events).Should(Receive(Equal(lrp)))
		})

		It("closes the events and errors channel when told to stop", func() {
			stop <- true
			stopped = true

			err := bbs.DesireLongRunningProcess(lrp)
			Ω(err).ShouldNot(HaveOccurred())

			Ω(events).Should(BeClosed())
			Ω(errors).Should(BeClosed())
		})
	})

	Describe("WatchForActualLongRunningProcesses", func() {
		var (
			events  <-chan models.LRP
			stop    chan<- bool
			errors  <-chan error
			stopped bool
		)

		lrp := models.LRP{ProcessGuid: "some-process-guid"}

		BeforeEach(func() {
			events, stop, errors = bbs.WatchForActualLongRunningProcesses()
		})

		AfterEach(func() {
			if !stopped {
				stop <- true
			}
		})

		It("sends an event down the pipe for creates", func() {
			err := bbs.ReportLongRunningProcessAsRunning(lrp)
			Ω(err).ShouldNot(HaveOccurred())

			Eventually(events).Should(Receive(Equal(lrp)))
		})

		It("sends an event down the pipe for updates", func() {
			err := bbs.ReportLongRunningProcessAsRunning(lrp)
			Ω(err).ShouldNot(HaveOccurred())

			Eventually(events).Should(Receive(Equal(lrp)))

			err = bbs.ReportLongRunningProcessAsRunning(lrp)
			Ω(err).ShouldNot(HaveOccurred())

			Eventually(events).Should(Receive(Equal(lrp)))
		})

		It("closes the events and errors channel when told to stop", func() {
			stop <- true
			stopped = true

			err := bbs.ReportLongRunningProcessAsRunning(lrp)
			Ω(err).ShouldNot(HaveOccurred())

			Ω(events).Should(BeClosed())
			Ω(errors).Should(BeClosed())
		})
	})

	Describe("GetAllDesiredLongRunningProcesses", func() {
		lrp1 := models.DesiredLRP{ProcessGuid: "guid1"}
		lrp2 := models.DesiredLRP{ProcessGuid: "guid2"}
		lrp3 := models.DesiredLRP{ProcessGuid: "guid3"}

		BeforeEach(func() {
			err := bbs.DesireLongRunningProcess(lrp1)
			Ω(err).ShouldNot(HaveOccurred())

			err = bbs.DesireLongRunningProcess(lrp2)
			Ω(err).ShouldNot(HaveOccurred())

			err = bbs.DesireLongRunningProcess(lrp3)
			Ω(err).ShouldNot(HaveOccurred())
		})

		It("returns all desired long running processes", func() {
			all, err := bbs.GetAllDesiredLongRunningProcesses()
			Ω(err).ShouldNot(HaveOccurred())

			Ω(all).Should(Equal([]models.DesiredLRP{lrp1, lrp2, lrp3}))
		})
	})

	Describe("GetAllActualLongRunningProcesses", func() {
		lrp1 := models.LRP{ProcessGuid: "guid1", Index: 1, InstanceGuid: "some-instance-guid"}
		lrp2 := models.LRP{ProcessGuid: "guid2", Index: 2, InstanceGuid: "some-instance-guid"}
		lrp3 := models.LRP{ProcessGuid: "guid3", Index: 2, InstanceGuid: "some-instance-guid"}

		BeforeEach(func() {
			err := bbs.ReportLongRunningProcessAsRunning(lrp1)
			Ω(err).ShouldNot(HaveOccurred())

			err = bbs.ReportLongRunningProcessAsRunning(lrp2)
			Ω(err).ShouldNot(HaveOccurred())

			err = bbs.ReportLongRunningProcessAsRunning(lrp3)
			Ω(err).ShouldNot(HaveOccurred())
		})

		It("returns all actual long running processes", func() {
			all, err := bbs.GetAllActualLongRunningProcesses()
			Ω(err).ShouldNot(HaveOccurred())

			Ω(all).Should(Equal([]models.LRP{lrp1, lrp2, lrp3}))
		})
	})
})
