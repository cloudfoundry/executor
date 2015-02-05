package containermetrics_test

import (
	"errors"
	"time"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/executor/containermetrics"
	efakes "github.com/cloudfoundry-incubator/executor/fakes"
	msfake "github.com/cloudfoundry/dropsonde/metric_sender/fake"
	dmetrics "github.com/cloudfoundry/dropsonde/metrics"
	"github.com/pivotal-golang/clock/fakeclock"
	"github.com/pivotal-golang/lager/lagertest"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/ginkgomon"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("StatsReporter", func() {
	var (
		logger *lagertest.TestLogger

		interval           time.Duration
		fakeClock          *fakeclock.FakeClock
		fakeExecutorClient *efakes.FakeClient
		fakeMetricSender   *msfake.FakeMetricSender

		workingListContainersStub func(executor.Tags) ([]executor.Container, error)

		process ifrit.Process
	)

	BeforeEach(func() {
		logger = lagertest.NewTestLogger("test")

		interval = 10 * time.Second
		fakeClock = fakeclock.NewFakeClock(time.Unix(123, 456))
		fakeExecutorClient = new(efakes.FakeClient)

		fakeMetricSender = msfake.NewFakeMetricSender()
		dmetrics.Initialize(fakeMetricSender)

		containerResults := make(chan []executor.Container, 10)

		one := 1
		containerResults <- []executor.Container{
			{
				Guid: "guid-without-index",

				MemoryUsageInBytes: 123,
				DiskUsageInBytes:   456,
				TimeSpentInCPU:     100 * time.Second,

				MetricsConfig: executor.MetricsConfig{
					Guid: "metrics-guid-without-index",
				},
			},
			{
				Guid: "guid-with-no-metrics-guid",

				MemoryUsageInBytes: 1023,
				DiskUsageInBytes:   4056,
				TimeSpentInCPU:     1000 * time.Second,
			},
			{
				Guid: "guid-with-index",

				MemoryUsageInBytes: 321,
				DiskUsageInBytes:   654,
				TimeSpentInCPU:     100 * time.Second,

				MetricsConfig: executor.MetricsConfig{
					Guid:  "metrics-guid-with-index",
					Index: &one,
				},
			},
		}

		containerResults <- []executor.Container{
			{
				Guid: "guid-without-index",

				MemoryUsageInBytes: 1230,
				DiskUsageInBytes:   4560,
				TimeSpentInCPU:     105 * time.Second,

				MetricsConfig: executor.MetricsConfig{
					Guid: "metrics-guid-without-index",
				},
			},
			{
				Guid: "guid-with-index",

				MemoryUsageInBytes: 3210,
				DiskUsageInBytes:   6540,
				TimeSpentInCPU:     110 * time.Second,

				MetricsConfig: executor.MetricsConfig{
					Guid:  "metrics-guid-with-index",
					Index: &one,
				},
			},
		}

		containerResults <- []executor.Container{
			{
				Guid: "guid-without-index",

				MemoryUsageInBytes: 12300,
				DiskUsageInBytes:   45600,
				TimeSpentInCPU:     107 * time.Second,

				MetricsConfig: executor.MetricsConfig{
					Guid: "metrics-guid-without-index",
				},
			},
			{
				Guid: "guid-with-index",

				MemoryUsageInBytes: 32100,
				DiskUsageInBytes:   65400,
				TimeSpentInCPU:     112 * time.Second,

				MetricsConfig: executor.MetricsConfig{
					Guid:  "metrics-guid-with-index",
					Index: &one,
				},
			},
		}

		workingListContainersStub = func(executor.Tags) ([]executor.Container, error) {
			return <-containerResults, nil
		}

		fakeExecutorClient.ListContainersStub = workingListContainersStub

		process = ifrit.Invoke(containermetrics.NewStatsReporter(logger, interval, fakeClock, fakeExecutorClient))
	})

	AfterEach(func() {
		ginkgomon.Interrupt(process)
	})

	Context("when the interval elapses", func() {
		BeforeEach(func() {
			fakeClock.Increment(interval)
			Eventually(fakeExecutorClient.ListContainersCallCount).Should(Equal(1))
		})

		It("emits memory and disk usage for each container, but no CPU", func() {
			Eventually(func() msfake.ContainerMetric {
				return fakeMetricSender.GetContainerMetric("metrics-guid-without-index")
			}).Should(Equal(msfake.ContainerMetric{
				ApplicationId: "metrics-guid-without-index",
				InstanceIndex: -1,
				CpuPercentage: 0.0,
				MemoryBytes:   123,
				DiskBytes:     456,
			}))

			Eventually(func() msfake.ContainerMetric {
				return fakeMetricSender.GetContainerMetric("metrics-guid-with-index")
			}).Should(Equal(msfake.ContainerMetric{
				ApplicationId: "metrics-guid-with-index",
				InstanceIndex: 1,
				CpuPercentage: 0.0,
				MemoryBytes:   321,
				DiskBytes:     654,
			}))
		})

		It("does not emit anything for containers with no metrics guid", func() {
			Consistently(func() msfake.ContainerMetric {
				return fakeMetricSender.GetContainerMetric("")
			}).Should(BeZero())
		})

		Context("and the interval elapses again", func() {
			BeforeEach(func() {
				fakeClock.Increment(interval)
				Eventually(fakeExecutorClient.ListContainersCallCount).Should(Equal(2))
			})

			It("emits the new memory and disk usage, and the computed CPU percent", func() {
				Eventually(func() msfake.ContainerMetric {
					return fakeMetricSender.GetContainerMetric("metrics-guid-without-index")
				}).Should(Equal(msfake.ContainerMetric{
					ApplicationId: "metrics-guid-without-index",
					InstanceIndex: -1,
					CpuPercentage: 50.0,
					MemoryBytes:   1230,
					DiskBytes:     4560,
				}))

				Eventually(func() msfake.ContainerMetric {
					return fakeMetricSender.GetContainerMetric("metrics-guid-with-index")
				}).Should(Equal(msfake.ContainerMetric{
					ApplicationId: "metrics-guid-with-index",
					InstanceIndex: 1,
					CpuPercentage: 100.0,
					MemoryBytes:   3210,
					DiskBytes:     6540,
				}))
			})

			Context("and the interval elapses again", func() {
				BeforeEach(func() {
					fakeClock.Increment(interval)
					Eventually(fakeExecutorClient.ListContainersCallCount).Should(Equal(3))
				})

				It("emits the new memory and disk usage, and the computed CPU percent", func() {
					Eventually(func() msfake.ContainerMetric {
						return fakeMetricSender.GetContainerMetric("metrics-guid-without-index")
					}).Should(Equal(msfake.ContainerMetric{
						ApplicationId: "metrics-guid-without-index",
						InstanceIndex: -1,
						CpuPercentage: 20.0,
						MemoryBytes:   12300,
						DiskBytes:     45600,
					}))

					Eventually(func() msfake.ContainerMetric {
						return fakeMetricSender.GetContainerMetric("metrics-guid-with-index")
					}).Should(Equal(msfake.ContainerMetric{
						ApplicationId: "metrics-guid-with-index",
						InstanceIndex: 1,
						CpuPercentage: 20.0,
						MemoryBytes:   32100,
						DiskBytes:     65400,
					}))
				})
			})
		})
	})

	Context("when listing containers fails", func() {
		BeforeEach(func() {
			fakeExecutorClient.ListContainersReturns(nil, errors.New("nope"))
			fakeClock.Increment(interval)
			Eventually(fakeExecutorClient.ListContainersCallCount).Should(Equal(1))
		})

		It("does not blow up", func() {
			Consistently(process.Wait()).ShouldNot(Receive())
		})

		Context("and the interval elapses again, and it works that time", func() {
			BeforeEach(func() {
				fakeExecutorClient.ListContainersStub = workingListContainersStub
				fakeClock.Increment(interval)
				Eventually(fakeExecutorClient.ListContainersCallCount).Should(Equal(2))
			})

			It("processes the containers happily", func() {
				Eventually(func() msfake.ContainerMetric {
					return fakeMetricSender.GetContainerMetric("metrics-guid-without-index")
				}).Should(Equal(msfake.ContainerMetric{
					ApplicationId: "metrics-guid-without-index",
					InstanceIndex: -1,
					CpuPercentage: 0.0,
					MemoryBytes:   123,
					DiskBytes:     456,
				}))

				Eventually(func() msfake.ContainerMetric {
					return fakeMetricSender.GetContainerMetric("metrics-guid-with-index")
				}).Should(Equal(msfake.ContainerMetric{
					ApplicationId: "metrics-guid-with-index",
					InstanceIndex: 1,
					CpuPercentage: 0.0,
					MemoryBytes:   321,
					DiskBytes:     654,
				}))
			})
		})
	})
})
