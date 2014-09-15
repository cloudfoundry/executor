package metrics_test

import (
	"errors"
	"time"

	"github.com/cloudfoundry-incubator/executor/api"
	. "github.com/cloudfoundry-incubator/executor/metrics"
	"github.com/cloudfoundry-incubator/executor/metrics/fakes"
	"github.com/cloudfoundry-incubator/executor/registry"
	"github.com/cloudfoundry-incubator/garden/warden"
	wfakes "github.com/cloudfoundry-incubator/garden/warden/fakes"
	"github.com/cloudfoundry/dropsonde/autowire/metrics"
	"github.com/cloudfoundry/dropsonde/metric_sender/fake"
	"github.com/tedsuo/ifrit"
)

// a bit of grace time for eventuallys
const aBit = 50 * time.Millisecond
const megabyte = 1024 * 1024

var _ = Describe("Reporter", func() {
	var (
		reportInterval time.Duration
		sender         *fake.FakeMetricSender
		executorSource *fakes.FakeExecutorSource
		actualSource   *fakes.FakeActualSource

		reporter ifrit.Process
	)

	BeforeEach(func() {
		reportInterval = 100 * time.Millisecond
		executorSource = new(fakes.FakeExecutorSource)
		actualSource = new(fakes.FakeActualSource)

		sender = fake.NewFakeMetricSender()
		metrics.Initialize(sender)

		executorSource.TotalCapacityReturns(registry.Capacity{
			MemoryMB:   1024,
			DiskMB:     2048,
			Containers: 4096,
		})

		executorSource.CurrentCapacityReturns(registry.Capacity{
			MemoryMB:   128,
			DiskMB:     256,
			Containers: 512,
		})

		executorSource.GetAllContainersReturns([]api.Container{
			{Guid: "container-1"},
			{Guid: "container-2"},
			{Guid: "container-3"},
		})

		actualSource.ContainersReturns([]warden.Container{
			new(wfakes.FakeContainer),
			new(wfakes.FakeContainer),
			new(wfakes.FakeContainer),
		}, nil)
	})

	JustBeforeEach(func() {
		reporter = ifrit.Envoke(&Reporter{
			ExecutorSource: executorSource,
			ActualSource:   actualSource,
			Interval:       reportInterval,
		})
	})

	It("reports the current capacity on the given interval", func() {
		Eventually(func() fake.Metric {
			return sender.GetValue("capacity.total.memory")
		}, reportInterval+aBit).Should(Equal(fake.Metric{
			Value: 1024 * megabyte,
			Unit:  "B",
		}))

		Eventually(func() fake.Metric {
			return sender.GetValue("capacity.total.disk")
		}, reportInterval+aBit).Should(Equal(fake.Metric{
			Value: 2048 * megabyte,
			Unit:  "B",
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
			Value: 128 * megabyte,
			Unit:  "B",
		}))

		Eventually(func() fake.Metric {
			return sender.GetValue("capacity.remaining.disk")
		}, reportInterval+aBit).Should(Equal(fake.Metric{
			Value: 256 * megabyte,
			Unit:  "B",
		}))

		Eventually(func() fake.Metric {
			return sender.GetValue("capacity.remaining.containers")
		}, reportInterval+aBit).Should(Equal(fake.Metric{
			Value: 512,
			Unit:  "Metric",
		}))

		Eventually(func() fake.Metric {
			return sender.GetValue("containers.expected")
		}, reportInterval+aBit).Should(Equal(fake.Metric{
			Value: 3,
			Unit:  "Metric",
		}))

		Eventually(func() fake.Metric {
			return sender.GetValue("containers.actual")
		}, reportInterval+aBit).Should(Equal(fake.Metric{
			Value: 3,
			Unit:  "Metric",
		}))

		executorSource.CurrentCapacityReturns(registry.Capacity{
			MemoryMB:   129,
			DiskMB:     257,
			Containers: 513,
		})

		executorSource.GetAllContainersReturns([]api.Container{
			{Guid: "container-1"},
			{Guid: "container-2"},
		})

		actualSource.ContainersReturns([]warden.Container{
			new(wfakes.FakeContainer),
			new(wfakes.FakeContainer),
		}, nil)

		Eventually(func() fake.Metric {
			return sender.GetValue("capacity.remaining.memory")
		}, reportInterval+aBit).Should(Equal(fake.Metric{
			Value: 129 * megabyte,
			Unit:  "B",
		}))

		Eventually(func() fake.Metric {
			return sender.GetValue("capacity.remaining.disk")
		}, reportInterval+aBit).Should(Equal(fake.Metric{
			Value: 257 * megabyte,
			Unit:  "B",
		}))

		Eventually(func() fake.Metric {
			return sender.GetValue("capacity.remaining.containers")
		}, reportInterval+aBit).Should(Equal(fake.Metric{
			Value: 513,
			Unit:  "Metric",
		}))

		Eventually(func() fake.Metric {
			return sender.GetValue("containers.expected")
		}, reportInterval+aBit).Should(Equal(fake.Metric{
			Value: 2,
			Unit:  "Metric",
		}))

		Eventually(func() fake.Metric {
			return sender.GetValue("containers.actual")
		}, reportInterval+aBit).Should(Equal(fake.Metric{
			Value: 2,
			Unit:  "Metric",
		}))
	})

	Context("when getting the actual container count fails", func() {
		BeforeEach(func() {
			actualSource.ContainersReturns(nil, errors.New("oh no!"))
		})

		It("reports garden.containers as -1", func() {
			Eventually(func() fake.Metric {
				return sender.GetValue("containers.actual")
			}, reportInterval+aBit).Should(Equal(fake.Metric{
				Value: -1,
				Unit:  "Metric",
			}))
		})
	})
})
