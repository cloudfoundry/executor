package metrics_test

import (
	"errors"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/executor/depot/metrics"
	"github.com/cloudfoundry-incubator/executor/fakes"
	"github.com/cloudfoundry/dropsonde/metric_sender/fake"
	dropsonde_metrics "github.com/cloudfoundry/dropsonde/metrics"
	"github.com/pivotal-golang/lager/lagertest"
	"github.com/tedsuo/ifrit"
)

// a bit of grace time for eventuallys
const aBit = 50 * time.Millisecond

var _ = Describe("Reporter", func() {
	var (
		reportInterval time.Duration
		sender         *fake.FakeMetricSender
		executorClient *fakes.FakeClient

		reporter ifrit.Process
	)

	BeforeEach(func() {
		reportInterval = 100 * time.Millisecond
		executorClient = new(fakes.FakeClient)

		sender = fake.NewFakeMetricSender()
		dropsonde_metrics.Initialize(sender)

		executorClient.TotalResourcesReturns(executor.ExecutorResources{
			MemoryMB:   1024,
			DiskMB:     2048,
			Containers: 4096,
		}, nil)

		executorClient.RemainingResourcesReturns(executor.ExecutorResources{
			MemoryMB:   128,
			DiskMB:     256,
			Containers: 512,
		}, nil)

		executorClient.ListContainersReturns([]executor.Container{
			{Guid: "container-1"},
			{Guid: "container-2"},
			{Guid: "container-3"},
		}, nil)
	})

	JustBeforeEach(func() {
		reporter = ifrit.Envoke(&metrics.Reporter{
			ExecutorSource: executorClient,
			Interval:       reportInterval,
			Logger:         lagertest.NewTestLogger("test"),
		})
	})

	It("reports the current capacity on the given interval", func() {
		Eventually(func() fake.Metric {
			return sender.GetValue("CapacityTotalMemory")
		}, reportInterval+aBit).Should(Equal(fake.Metric{
			Value: 1024,
			Unit:  "MiB",
		}))

		Eventually(func() fake.Metric {
			return sender.GetValue("CapacityTotalDisk")
		}, reportInterval+aBit).Should(Equal(fake.Metric{
			Value: 2048,
			Unit:  "MiB",
		}))

		Eventually(func() fake.Metric {
			return sender.GetValue("CapacityTotalContainers")
		}, reportInterval+aBit).Should(Equal(fake.Metric{
			Value: 4096,
			Unit:  "Metric",
		}))

		Eventually(func() fake.Metric {
			return sender.GetValue("CapacityRemainingMemory")
		}, reportInterval+aBit).Should(Equal(fake.Metric{
			Value: 128,
			Unit:  "MiB",
		}))

		Eventually(func() fake.Metric {
			return sender.GetValue("CapacityRemainingDisk")
		}, reportInterval+aBit).Should(Equal(fake.Metric{
			Value: 256,
			Unit:  "MiB",
		}))

		Eventually(func() fake.Metric {
			return sender.GetValue("CapacityRemainingContainers")
		}, reportInterval+aBit).Should(Equal(fake.Metric{
			Value: 512,
			Unit:  "Metric",
		}))

		Eventually(func() fake.Metric {
			return sender.GetValue("ContainerCount")
		}, reportInterval+aBit).Should(Equal(fake.Metric{
			Value: 3,
			Unit:  "Metric",
		}))

		executorClient.RemainingResourcesReturns(executor.ExecutorResources{
			MemoryMB:   129,
			DiskMB:     257,
			Containers: 513,
		}, nil)

		executorClient.ListContainersReturns([]executor.Container{
			{Guid: "container-1"},
			{Guid: "container-2"},
		}, nil)

		Eventually(func() fake.Metric {
			return sender.GetValue("CapacityRemainingMemory")
		}, reportInterval+aBit).Should(Equal(fake.Metric{
			Value: 129,
			Unit:  "MiB",
		}))

		Eventually(func() fake.Metric {
			return sender.GetValue("CapacityRemainingDisk")
		}, reportInterval+aBit).Should(Equal(fake.Metric{
			Value: 257,
			Unit:  "MiB",
		}))

		Eventually(func() fake.Metric {
			return sender.GetValue("CapacityRemainingContainers")
		}, reportInterval+aBit).Should(Equal(fake.Metric{
			Value: 513,
			Unit:  "Metric",
		}))

		Eventually(func() fake.Metric {
			return sender.GetValue("ContainerCount")
		}, reportInterval+aBit).Should(Equal(fake.Metric{
			Value: 2,
			Unit:  "Metric",
		}))
	})

	Context("when getting the containers fails", func() {
		BeforeEach(func() {
			executorClient.ListContainersReturns(nil, errors.New("oh no!"))
		})

		It("reports garden.containers as -1", func() {
			Eventually(func() fake.Metric {
				return sender.GetValue("ContainerCount")
			}, reportInterval+aBit).Should(Equal(fake.Metric{
				Value: -1,
				Unit:  "Metric",
			}))
		})
	})
})
