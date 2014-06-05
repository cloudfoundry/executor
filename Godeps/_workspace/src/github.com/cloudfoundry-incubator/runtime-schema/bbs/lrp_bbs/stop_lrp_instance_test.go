package lrp_bbs_test

import (
	"github.com/cloudfoundry-incubator/runtime-schema/models"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("StopLRPInstance", func() {
	var stopInstance models.StopLRPInstance

	BeforeEach(func() {
		stopInstance = models.StopLRPInstance{
			ProcessGuid:  "some-process-guid",
			InstanceGuid: "some-instance-guid",
			Index:        5678,
		}
	})

	Describe("RequestStopLRPInstance", func() {
		It("creates /v1/stop-instance/<instance-guid>", func() {
			err := bbs.RequestStopLRPInstance(stopInstance)
			Ω(err).ShouldNot(HaveOccurred())

			node, err := etcdClient.Get("/v1/stop-instance/some-instance-guid")
			Ω(err).ShouldNot(HaveOccurred())

			Ω(node.TTL).Should(BeNumerically(">", 0))
			Ω(node.Value).Should(Equal(stopInstance.ToJSON()))
		})

		Context("when the key already exists", func() {
			It("sets it again", func() {
				err := bbs.RequestStopLRPInstance(stopInstance)
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.RequestStopLRPInstance(stopInstance)
				Ω(err).ShouldNot(HaveOccurred())
			})
		})

		Context("when the store is out of commission", func() {
			itRetriesUntilStoreComesBack(func() error {
				return bbs.RequestStopLRPInstance(stopInstance)
			})
		})
	})

	Describe("RequestStopLRPInstances", func() {
		It("creates multiple /v1/stop-instance/<instance-guid> keys", func() {
			anotherStopInstance := models.StopLRPInstance{
				ProcessGuid:  "some-other-process-guid",
				InstanceGuid: "some-other-instance-guid",
				Index:        1234,
			}

			err := bbs.RequestStopLRPInstances([]models.StopLRPInstance{stopInstance, anotherStopInstance})
			Ω(err).ShouldNot(HaveOccurred())

			node, err := etcdClient.Get("/v1/stop-instance/some-instance-guid")
			Ω(err).ShouldNot(HaveOccurred())

			Ω(node.TTL).Should(BeNumerically(">", 0))
			Ω(node.Value).Should(Equal(stopInstance.ToJSON()))

			anotherNode, err := etcdClient.Get("/v1/stop-instance/some-other-instance-guid")
			Ω(err).ShouldNot(HaveOccurred())

			Ω(anotherNode.TTL).Should(BeNumerically(">", 0))
			Ω(anotherNode.Value).Should(Equal(anotherStopInstance.ToJSON()))
		})

		Context("when the key already exists", func() {
			It("sets it again", func() {
				err := bbs.RequestStopLRPInstances([]models.StopLRPInstance{stopInstance})
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.RequestStopLRPInstances([]models.StopLRPInstance{stopInstance})
				Ω(err).ShouldNot(HaveOccurred())
			})
		})

		Context("when the store is out of commission", func() {
			itRetriesUntilStoreComesBack(func() error {
				return bbs.RequestStopLRPInstances([]models.StopLRPInstance{stopInstance})
			})
		})
	})

	Describe("GetAllStopLRPInstances", func() {
		It("gets all stop instances", func() {
			stopInstance1 := models.StopLRPInstance{
				InstanceGuid: "some-instance-guid-1",
			}
			stopInstance2 := models.StopLRPInstance{
				InstanceGuid: "some-instance-guid-2+",
			}

			err := bbs.RequestStopLRPInstance(stopInstance1)
			Ω(err).ShouldNot(HaveOccurred())

			err = bbs.RequestStopLRPInstance(stopInstance2)
			Ω(err).ShouldNot(HaveOccurred())

			stopInstances, err := bbs.GetAllStopLRPInstances()
			Ω(err).ShouldNot(HaveOccurred())

			Ω(stopInstances).Should(HaveLen(2))

			Ω(stopInstances).Should(ContainElement(stopInstance1))
			Ω(stopInstances).Should(ContainElement(stopInstance2))
		})
	})

	Describe("ResolveStopLRPInstance", func() {
		Context("the happy path", func() {
			BeforeEach(func() {
				err := bbs.RequestStopLRPInstance(stopInstance)
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.ReportActualLRPAsRunning(models.ActualLRP{
					ProcessGuid:  stopInstance.ProcessGuid,
					InstanceGuid: stopInstance.InstanceGuid,
					Index:        stopInstance.Index,
				}, "executor-id")
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.ResolveStopLRPInstance(stopInstance)
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("removes the StopLRPInstance", func() {
				Ω(bbs.GetAllStopLRPInstances()).Should(BeEmpty())
			})

			It("removes the associate ActualLRP", func() {
				Ω(bbs.GetAllActualLRPs()).Should(BeEmpty())
			})
		})

		Context("when the StopLRPInstance does not exist", func() {
			BeforeEach(func() {
				err := bbs.ReportActualLRPAsRunning(models.ActualLRP{
					ProcessGuid:  stopInstance.ProcessGuid,
					InstanceGuid: stopInstance.InstanceGuid,
					Index:        stopInstance.Index,
				}, "executor-id")
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("does not error, and still removes the ActualLRP", func() {
				err := bbs.ResolveStopLRPInstance(stopInstance)
				Ω(err).ShouldNot(HaveOccurred())

				Ω(bbs.GetAllActualLRPs()).Should(BeEmpty())
			})
		})

		Context("when the ActualLRP does not exist", func() {
			BeforeEach(func() {
				err := bbs.RequestStopLRPInstance(stopInstance)
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("does not error, and still removes the StopLRPInstance", func() {
				err := bbs.ResolveStopLRPInstance(stopInstance)
				Ω(err).ShouldNot(HaveOccurred())

				Ω(bbs.GetAllStopLRPInstances()).Should(BeEmpty())
			})
		})

		Context("when the store is out of commission", func() {
			BeforeEach(func() {
				err := bbs.RequestStopLRPInstance(stopInstance)
				Ω(err).ShouldNot(HaveOccurred())
			})

			itRetriesUntilStoreComesBack(func() error {
				return bbs.ResolveStopLRPInstance(stopInstance)
			})
		})
	})

	Describe("WatchForStopLRPInstance", func() {
		var (
			events       <-chan models.StopLRPInstance
			stop         chan<- bool
			errors       <-chan error
			stopInstance models.StopLRPInstance
		)

		BeforeEach(func() {
			events, stop, errors = bbs.WatchForStopLRPInstance()
		})

		AfterEach(func() {
			stop <- true
		})

		It("sends an event down the pipe for creates", func() {
			err := bbs.RequestStopLRPInstance(stopInstance)
			Ω(err).ShouldNot(HaveOccurred())

			Eventually(events).Should(Receive(Equal(stopInstance)))
		})

		It("sends an event down the pipe for updates", func() {
			err := bbs.RequestStopLRPInstance(stopInstance)
			Ω(err).ShouldNot(HaveOccurred())

			Eventually(events).Should(Receive(Equal(stopInstance)))

			err = bbs.RequestStopLRPInstance(stopInstance)
			Ω(err).ShouldNot(HaveOccurred())

			Eventually(events).Should(Receive(Equal(stopInstance)))
		})

		It("does not send an event down the pipe for deletes", func() {
			err := bbs.RequestStopLRPInstance(stopInstance)
			Ω(err).ShouldNot(HaveOccurred())

			Eventually(events).Should(Receive(Equal(stopInstance)))

			err = bbs.ResolveStopLRPInstance(stopInstance)
			Ω(err).ShouldNot(HaveOccurred())

			Consistently(events).ShouldNot(Receive())
		})
	})
})
