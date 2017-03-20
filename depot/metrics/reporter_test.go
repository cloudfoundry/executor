package metrics_test

import (
	"errors"
	"os"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"code.cloudfoundry.org/clock/fakeclock"
	"code.cloudfoundry.org/executor"
	"code.cloudfoundry.org/executor/depot/metrics"
	"code.cloudfoundry.org/executor/fakes"
	"code.cloudfoundry.org/lager/lagertest"
	mfakes "code.cloudfoundry.org/loggregator_v2/fakes"
	"github.com/cloudfoundry/dropsonde/metric_sender/fake"
	dropsonde_metrics "github.com/cloudfoundry/dropsonde/metrics"
	"github.com/tedsuo/ifrit"
)

var _ = Describe("Reporter", func() {
	var (
		reportInterval   time.Duration
		sender           *fake.FakeMetricSender
		executorClient   *fakes.FakeClient
		fakeClock        *fakeclock.FakeClock
		fakeMetronClient *mfakes.FakeClient

		reporter ifrit.Process
		logger   *lagertest.TestLogger
	)

	BeforeEach(func() {
		logger = lagertest.NewTestLogger("test")
		reportInterval = 1 * time.Millisecond
		executorClient = new(fakes.FakeClient)

		sender = fake.NewFakeMetricSender()
		dropsonde_metrics.Initialize(sender, nil)
		fakeClock = fakeclock.NewFakeClock(time.Now())
		fakeMetronClient = new(mfakes.FakeClient)

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
		reporter = ifrit.Invoke(&metrics.Reporter{
			ExecutorSource: executorClient,
			Interval:       reportInterval,
			Clock:          fakeClock,
			Logger:         logger,
			MetronClient:   fakeMetronClient,
		})
		fakeClock.WaitForWatcherAndIncrement(reportInterval)
	})

	AfterEach(func() {
		reporter.Signal(os.Interrupt)
		Eventually(reporter.Wait()).Should(Receive())
	})

	It("reports the current capacity on the given interval", func() {
		Eventually(fakeMetronClient.SendMebiBytesCallCount).Should(Equal(4))
		Eventually(func() []interface{} {
			metric, value := fakeMetronClient.SendMebiBytesArgsForCall(0)
			return []interface{}{metric, value}
		}).Should(Equal([]interface{}{"CapacityTotalMemory", 1024}))

		Eventually(func() []interface{} {
			metric, value := fakeMetronClient.SendMebiBytesArgsForCall(1)
			return []interface{}{metric, value}
		}).Should(Equal([]interface{}{"CapacityTotalDisk", 2048}))

		Eventually(func() []interface{} {
			metric, value := fakeMetronClient.SendMebiBytesArgsForCall(2)
			return []interface{}{metric, value}
		}).Should(Equal([]interface{}{"CapacityRemainingMemory", 128}))

		Eventually(func() []interface{} {
			metric, value := fakeMetronClient.SendMebiBytesArgsForCall(3)
			return []interface{}{metric, value}
		}).Should(Equal([]interface{}{"CapacityRemainingDisk", 256}))

		Eventually(func() []interface{} {
			metric, value := fakeMetronClient.SendMetricArgsForCall(0)
			return []interface{}{metric, value}
		}).Should(Equal([]interface{}{"CapacityTotalContainers", 4096}))

		Eventually(func() []interface{} {
			metric, value := fakeMetronClient.SendMetricArgsForCall(1)
			return []interface{}{metric, value}
		}).Should(Equal([]interface{}{"CapacityRemainingContainers", 512}))

		Eventually(func() []interface{} {
			metric, value := fakeMetronClient.SendMetricArgsForCall(2)
			return []interface{}{metric, value}
		}).Should(Equal([]interface{}{"ContainerCount", 3}))

		executorClient.RemainingResourcesReturns(executor.ExecutorResources{
			MemoryMB:   129,
			DiskMB:     257,
			Containers: 513,
		}, nil)

		executorClient.ListContainersReturns([]executor.Container{
			{Guid: "container-1"},
			{Guid: "container-2"},
		}, nil)

		fakeClock.Increment(reportInterval)

		Eventually(fakeMetronClient.SendMebiBytesCallCount).Should(Equal(8))
		Eventually(func() []interface{} {
			metric, value := fakeMetronClient.SendMebiBytesArgsForCall(6)
			return []interface{}{metric, value}
		}).Should(Equal([]interface{}{"CapacityRemainingMemory", 129}))

		Eventually(func() []interface{} {
			metric, value := fakeMetronClient.SendMebiBytesArgsForCall(7)
			return []interface{}{metric, value}
		}).Should(Equal([]interface{}{"CapacityRemainingDisk", 257}))

		Eventually(func() []interface{} {
			metric, value := fakeMetronClient.SendMetricArgsForCall(4)
			return []interface{}{metric, value}
		}).Should(Equal([]interface{}{"CapacityRemainingContainers", 513}))

		Eventually(func() []interface{} {
			metric, value := fakeMetronClient.SendMetricArgsForCall(5)
			return []interface{}{metric, value}
		}).Should(Equal([]interface{}{"ContainerCount", 2}))
	})

	Context("when getting remaining resources fails", func() {
		BeforeEach(func() {
			executorClient.RemainingResourcesReturns(executor.ExecutorResources{}, errors.New("oh no!"))
		})

		It("sends missing remaining resources", func() {
			Eventually(fakeMetronClient.SendMebiBytesCallCount).Should(Equal(4))
			Eventually(func() []interface{} {
				metric, value := fakeMetronClient.SendMebiBytesArgsForCall(2)
				return []interface{}{metric, value}
			}).Should(Equal([]interface{}{"CapacityRemainingMemory", -1}))

			Eventually(func() []interface{} {
				metric, value := fakeMetronClient.SendMebiBytesArgsForCall(3)
				return []interface{}{metric, value}
			}).Should(Equal([]interface{}{"CapacityRemainingDisk", -1}))

			Eventually(func() []interface{} {
				metric, value := fakeMetronClient.SendMetricArgsForCall(1)
				return []interface{}{metric, value}
			}).Should(Equal([]interface{}{"CapacityRemainingContainers", -1}))
		})
	})

	Context("when getting total resources fails", func() {
		BeforeEach(func() {
			executorClient.TotalResourcesReturns(executor.ExecutorResources{}, errors.New("oh no!"))
		})

		It("sends missing total resources", func() {
			Eventually(fakeMetronClient.SendMebiBytesCallCount).Should(Equal(4))
			Eventually(func() []interface{} {
				metric, value := fakeMetronClient.SendMebiBytesArgsForCall(0)
				return []interface{}{metric, value}
			}).Should(Equal([]interface{}{"CapacityTotalMemory", -1}))

			Eventually(func() []interface{} {
				metric, value := fakeMetronClient.SendMebiBytesArgsForCall(1)
				return []interface{}{metric, value}
			}).Should(Equal([]interface{}{"CapacityTotalDisk", -1}))

			Eventually(func() []interface{} {
				metric, value := fakeMetronClient.SendMetricArgsForCall(0)
				return []interface{}{metric, value}
			}).Should(Equal([]interface{}{"CapacityTotalContainers", -1}))
		})
	})

	Context("when getting the containers fails", func() {
		BeforeEach(func() {
			executorClient.ListContainersReturns(nil, errors.New("oh no!"))
		})

		It("reports garden.containers as -1", func() {
			logger.Info("checking this stuff")
			Eventually(fakeMetronClient.SendMetricCallCount).Should(Equal(3))
			Eventually(func() []interface{} {
				metric, value := fakeMetronClient.SendMetricArgsForCall(2)
				return []interface{}{metric, value}
			}).Should(Equal([]interface{}{"ContainerCount", -1}))
		})
	})
})
