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

type listContainerResults struct {
	containers []executor.Container
	err        error
}

type metricsResults struct {
	metrics executor.ContainerMetrics
	err     error
}

var _ = Describe("StatsReporter", func() {
	var (
		logger *lagertest.TestLogger

		interval           time.Duration
		fakeClock          *fakeclock.FakeClock
		fakeExecutorClient *efakes.FakeClient
		fakeMetricSender   *msfake.FakeMetricSender

		metricsResults chan map[string]executor.Metrics
		process        ifrit.Process
	)

	sendResults := func() {
		metricsResults <- map[string]executor.Metrics{
			"guid-without-index": executor.Metrics{
				MetricsConfig: executor.MetricsConfig{Guid: "metrics-guid-without-index"},
				ContainerMetrics: executor.ContainerMetrics{
					MemoryUsageInBytes: 123,
					DiskUsageInBytes:   456,
					TimeSpentInCPU:     100 * time.Second,
				},
			},
			"guid-with-index": executor.Metrics{
				MetricsConfig: executor.MetricsConfig{Guid: "metrics-guid-with-index", Index: 1},
				ContainerMetrics: executor.ContainerMetrics{
					MemoryUsageInBytes: 321,
					DiskUsageInBytes:   654,
					TimeSpentInCPU:     100 * time.Second,
				},
			},
		}

		metricsResults <- map[string]executor.Metrics{
			"guid-without-index": executor.Metrics{
				MetricsConfig: executor.MetricsConfig{Guid: "metrics-guid-without-index"},
				ContainerMetrics: executor.ContainerMetrics{
					MemoryUsageInBytes: 1230,
					DiskUsageInBytes:   4560,
					TimeSpentInCPU:     105 * time.Second,
				},
			},
			"guid-with-index": executor.Metrics{
				MetricsConfig: executor.MetricsConfig{Guid: "metrics-guid-with-index", Index: 1},
				ContainerMetrics: executor.ContainerMetrics{
					MemoryUsageInBytes: 3210,
					DiskUsageInBytes:   6540,
					TimeSpentInCPU:     110 * time.Second,
				},
			},
		}

		metricsResults <- map[string]executor.Metrics{
			"guid-without-index": executor.Metrics{
				MetricsConfig: executor.MetricsConfig{Guid: "metrics-guid-without-index"},
				ContainerMetrics: executor.ContainerMetrics{
					MemoryUsageInBytes: 12300,
					DiskUsageInBytes:   45600,
					TimeSpentInCPU:     107 * time.Second,
				},
			},
			"guid-with-index": executor.Metrics{
				MetricsConfig: executor.MetricsConfig{Guid: "metrics-guid-with-index", Index: 1},
				ContainerMetrics: executor.ContainerMetrics{
					MemoryUsageInBytes: 32100,
					DiskUsageInBytes:   65400,
					TimeSpentInCPU:     112 * time.Second,
				},
			},
		}
	}

	BeforeEach(func() {
		logger = lagertest.NewTestLogger("test")

		interval = 10 * time.Second
		fakeClock = fakeclock.NewFakeClock(time.Unix(123, 456))
		fakeExecutorClient = new(efakes.FakeClient)

		fakeMetricSender = msfake.NewFakeMetricSender()
		dmetrics.Initialize(fakeMetricSender)

		metricsResults = make(chan map[string]executor.Metrics, 10)

		fakeExecutorClient.GetAllMetricsStub = func(tags executor.Tags) (map[string]executor.Metrics, error) {
			result, ok := <-metricsResults
			if !ok || result == nil {
				return nil, errors.New("closed")
			}
			return result, nil
		}

		process = ifrit.Invoke(containermetrics.NewStatsReporter(logger, interval, fakeClock, fakeExecutorClient))
	})

	AfterEach(func() {
		close(metricsResults)
		ginkgomon.Interrupt(process)
	})

	Context("when the interval elapses", func() {
		BeforeEach(func() {
			sendResults()

			fakeClock.Increment(interval)
			Eventually(fakeExecutorClient.GetAllMetricsCallCount).Should(Equal(1))
		})

		It("emits memory and disk usage for each container, but no CPU", func() {
			Eventually(func() msfake.ContainerMetric {
				return fakeMetricSender.GetContainerMetric("metrics-guid-without-index")
			}).Should(Equal(msfake.ContainerMetric{
				ApplicationId: "metrics-guid-without-index",
				InstanceIndex: 0,
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
				Eventually(fakeExecutorClient.GetAllMetricsCallCount).Should(Equal(2))
			})

			It("emits the new memory and disk usage, and the computed CPU percent", func() {
				Eventually(func() msfake.ContainerMetric {
					return fakeMetricSender.GetContainerMetric("metrics-guid-without-index")
				}).Should(Equal(msfake.ContainerMetric{
					ApplicationId: "metrics-guid-without-index",
					InstanceIndex: 0,
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
					Eventually(fakeExecutorClient.GetAllMetricsCallCount).Should(Equal(3))
				})

				It("emits the new memory and disk usage, and the computed CPU percent", func() {
					Eventually(func() msfake.ContainerMetric {
						return fakeMetricSender.GetContainerMetric("metrics-guid-without-index")
					}).Should(Equal(msfake.ContainerMetric{
						ApplicationId: "metrics-guid-without-index",
						InstanceIndex: 0,
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

	Context("when get all metrics fails", func() {
		BeforeEach(func() {
			metricsResults <- nil

			fakeClock.Increment(interval)
			Eventually(fakeExecutorClient.GetAllMetricsCallCount).Should(Equal(1))
		})

		It("does not blow up", func() {
			Consistently(process.Wait()).ShouldNot(Receive())
		})

		Context("and the interval elapses again, and it works that time", func() {
			BeforeEach(func() {
				sendResults()
				fakeClock.Increment(interval)
				Eventually(fakeExecutorClient.GetAllMetricsCallCount).Should(Equal(2))
			})

			It("processes the containers happily", func() {
				Eventually(func() msfake.ContainerMetric {
					return fakeMetricSender.GetContainerMetric("metrics-guid-without-index")
				}).Should(Equal(msfake.ContainerMetric{
					ApplicationId: "metrics-guid-without-index",
					InstanceIndex: 0,
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

	Context("when a container is no longer present", func() {
		metrics := func(metricsGuid string, index int, memory, disk uint64, cpuTime time.Duration) executor.Metrics {
			return executor.Metrics{
				MetricsConfig: executor.MetricsConfig{
					Guid:  metricsGuid,
					Index: index,
				},
				ContainerMetrics: executor.ContainerMetrics{
					MemoryUsageInBytes: memory,
					DiskUsageInBytes:   disk,
					TimeSpentInCPU:     cpuTime,
				},
			}
		}

		waitForMetrics := func(id string, instance int32, cpu float64, memory, disk uint64) {
			Eventually(func() msfake.ContainerMetric {
				return fakeMetricSender.GetContainerMetric(id)
			}).Should(Equal(msfake.ContainerMetric{
				ApplicationId: id,
				InstanceIndex: instance,
				CpuPercentage: cpu,
				MemoryBytes:   memory,
				DiskBytes:     disk,
			}))
		}

		It("only remembers the previous metrics", func() {
			metricsResults <- map[string]executor.Metrics{
				"container-guid-0": metrics("metrics-guid-0", 0, 128, 256, 10*time.Second),
				"container-guid-1": metrics("metrics-guid-1", 1, 256, 512, 10*time.Second),
			}

			fakeClock.Increment(interval)

			waitForMetrics("metrics-guid-0", 0, 0, 128, 256)
			waitForMetrics("metrics-guid-1", 1, 0, 256, 512)

			By("losing a container")
			metricsResults <- map[string]executor.Metrics{
				"container-guid-0": metrics("metrics-guid-0", 0, 256, 512, 30*time.Second),
			}

			fakeClock.Increment(interval)

			waitForMetrics("metrics-guid-0", 0, 200, 256, 512)
			waitForMetrics("metrics-guid-1", 1, 0, 256, 512)

			By("finding the container again")
			metricsResults <- map[string]executor.Metrics{
				"container-guid-0": metrics("metrics-guid-0", 0, 256, 512, 40*time.Second),
				"container-guid-1": metrics("metrics-guid-1", 1, 512, 1024, 10*time.Second),
			}

			fakeClock.Increment(interval)

			waitForMetrics("metrics-guid-0", 0, 100, 256, 512)
			waitForMetrics("metrics-guid-1", 1, 0, 512, 1024)
		})
	})
})
