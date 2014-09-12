package metrics_test

import (
	"time"

	"github.com/cloudfoundry-incubator/executor/api"
	. "github.com/cloudfoundry-incubator/executor/metrics"
	"github.com/cloudfoundry-incubator/executor/metrics/fakes"
	"github.com/cloudfoundry-incubator/executor/registry"
	"github.com/cloudfoundry/dropsonde/autowire/metrics"
	"github.com/cloudfoundry/dropsonde/metric_sender/fake"
	"github.com/tedsuo/ifrit"
)

// a bit of grace time for eventuallys
const aBit = 50 * time.Millisecond

var _ = Describe("Reporter", func() {
	var (
		reportInterval time.Duration
		sender         *fake.FakeMetricSender
		source         *fakes.FakeSource

		reporter ifrit.Process
	)

	BeforeEach(func() {
		reportInterval = 100 * time.Millisecond
		source = new(fakes.FakeSource)

		sender = fake.NewFakeMetricSender()
		metrics.Initialize(sender)

		source.TotalCapacityReturns(registry.Capacity{
			MemoryMB:   1024,
			DiskMB:     2048,
			Containers: 4096,
		})

		source.CurrentCapacityReturns(registry.Capacity{
			MemoryMB:   128,
			DiskMB:     256,
			Containers: 512,
		})

		source.GetAllContainersReturns([]api.Container{
			{Guid: "container-1"},
			{Guid: "container-2"},
			{Guid: "container-3"},
		})
	})

	JustBeforeEach(func() {
		reporter = ifrit.Envoke(&Reporter{
			Source:   source,
			Interval: reportInterval,
		})
	})

	It("reports the current capacity on the given interval", func() {
		Eventually(func() fake.Metric {
			return sender.GetValue("capacity.total.memory")
		}, reportInterval+aBit).Should(Equal(fake.Metric{
			Value: 1024,
			Unit:  "Metric",
		}))

		Eventually(func() fake.Metric {
			return sender.GetValue("capacity.total.disk")
		}, reportInterval+aBit).Should(Equal(fake.Metric{
			Value: 2048,
			Unit:  "Metric",
		}))

		Eventually(func() fake.Metric {
			return sender.GetValue("capacity.total.containers")
		}, reportInterval+aBit).Should(Equal(fake.Metric{
			Value: 4096,
			Unit:  "Metric",
		}))

		Eventually(func() fake.Metric {
			return sender.GetValue("capacity.remaining.memory")
		}, reportInterval+aBit).Should(Equal(fake.Metric{
			Value: 128,
			Unit:  "Metric",
		}))

		Eventually(func() fake.Metric {
			return sender.GetValue("capacity.remaining.disk")
		}, reportInterval+aBit).Should(Equal(fake.Metric{
			Value: 256,
			Unit:  "Metric",
		}))

		Eventually(func() fake.Metric {
			return sender.GetValue("capacity.remaining.containers")
		}, reportInterval+aBit).Should(Equal(fake.Metric{
			Value: 512,
			Unit:  "Metric",
		}))

		Eventually(func() fake.Metric {
			return sender.GetValue("containers")
		}, reportInterval+aBit).Should(Equal(fake.Metric{
			Value: 3,
			Unit:  "Metric",
		}))

		source.CurrentCapacityReturns(registry.Capacity{
			MemoryMB:   129,
			DiskMB:     257,
			Containers: 513,
		})

		source.GetAllContainersReturns([]api.Container{
			{Guid: "container-1"},
			{Guid: "container-2"},
		})

		Eventually(func() fake.Metric {
			return sender.GetValue("capacity.remaining.memory")
		}, reportInterval+aBit).Should(Equal(fake.Metric{
			Value: 129,
			Unit:  "Metric",
		}))

		Eventually(func() fake.Metric {
			return sender.GetValue("capacity.remaining.disk")
		}, reportInterval+aBit).Should(Equal(fake.Metric{
			Value: 257,
			Unit:  "Metric",
		}))

		Eventually(func() fake.Metric {
			return sender.GetValue("capacity.remaining.containers")
		}, reportInterval+aBit).Should(Equal(fake.Metric{
			Value: 513,
			Unit:  "Metric",
		}))

		Eventually(func() fake.Metric {
			return sender.GetValue("containers")
		}, reportInterval+aBit).Should(Equal(fake.Metric{
			Value: 2,
			Unit:  "Metric",
		}))
	})
})
