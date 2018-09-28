package containermetrics_test

import (
	"errors"
	"time"

	"code.cloudfoundry.org/clock/fakeclock"
	logging "code.cloudfoundry.org/diego-logging-client"
	mfakes "code.cloudfoundry.org/diego-logging-client/testhelpers"
	"code.cloudfoundry.org/executor"
	"code.cloudfoundry.org/executor/containermetrics"
	efakes "code.cloudfoundry.org/executor/fakes"
	"code.cloudfoundry.org/lager"
	"code.cloudfoundry.org/lager/lagertest"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/ginkgomon"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func megsToBytes(mem int) uint64 {
	return uint64(mem * 1024 * 1024)
}

var _ = Describe("StatsReporter", func() {
	var (
		logger *lagertest.TestLogger

		interval           time.Duration
		fakeClock          *fakeclock.FakeClock
		fakeExecutorClient *efakes.FakeClient
		fakeMetronClient   *mfakes.FakeIngressClient

		metricsResultsChan chan map[string]executor.Metrics
		listContainersChan chan []executor.Container
		process            ifrit.Process

		enableContainerProxy    bool
		proxyMemoryAllocationMB int
		reporter                *containermetrics.StatsReporter
	)

	sendResults := func() {
		metricsResultsChan <- map[string]executor.Metrics{
			"container-guid-without-metrics-guid": executor.Metrics{
				MetricsConfig: executor.MetricsConfig{Guid: ""},
				ContainerMetrics: executor.ContainerMetrics{
					MemoryUsageInBytes:                  megsToBytes(123),
					DiskUsageInBytes:                    megsToBytes(456),
					TimeSpentInCPU:                      100 * time.Second,
					MemoryLimitInBytes:                  megsToBytes(789),
					DiskLimitInBytes:                    megsToBytes(1024),
					ContainerAgeInNanoseconds:           1000,
					AbsoluteCPUEntitlementInNanoseconds: 2000,
				},
			},

			"container-guid-without-index": executor.Metrics{
				MetricsConfig: executor.MetricsConfig{Guid: "metrics-guid-without-index"},
				ContainerMetrics: executor.ContainerMetrics{
					MemoryUsageInBytes:                  megsToBytes(123),
					DiskUsageInBytes:                    megsToBytes(456),
					TimeSpentInCPU:                      100 * time.Second,
					MemoryLimitInBytes:                  megsToBytes(789),
					DiskLimitInBytes:                    megsToBytes(1024),
					ContainerAgeInNanoseconds:           1001,
					AbsoluteCPUEntitlementInNanoseconds: 2001,
				},
			},
			"container-guid-with-index": executor.Metrics{
				MetricsConfig: executor.MetricsConfig{Guid: "metrics-guid-with-index", Index: 1},
				ContainerMetrics: executor.ContainerMetrics{
					MemoryUsageInBytes:                  megsToBytes(321),
					DiskUsageInBytes:                    megsToBytes(654),
					TimeSpentInCPU:                      100 * time.Second,
					MemoryLimitInBytes:                  megsToBytes(987),
					DiskLimitInBytes:                    megsToBytes(2048),
					ContainerAgeInNanoseconds:           1002,
					AbsoluteCPUEntitlementInNanoseconds: 2002,
				},
			},
			"container-guid-without-preloaded-rootfs": executor.Metrics{
				MetricsConfig: executor.MetricsConfig{Guid: "metrics-guid-without-preloaded-rootfs"},
				ContainerMetrics: executor.ContainerMetrics{
					MemoryUsageInBytes:                  megsToBytes(345),
					DiskUsageInBytes:                    megsToBytes(456),
					TimeSpentInCPU:                      100 * time.Second,
					MemoryLimitInBytes:                  megsToBytes(678),
					DiskLimitInBytes:                    megsToBytes(2048),
					ContainerAgeInNanoseconds:           1003,
					AbsoluteCPUEntitlementInNanoseconds: 2003,
				},
			},
		}

		metricsResultsChan <- map[string]executor.Metrics{
			"container-guid-without-metrics-guid": executor.Metrics{
				MetricsConfig: executor.MetricsConfig{Guid: ""},
				ContainerMetrics: executor.ContainerMetrics{
					MemoryUsageInBytes:                  megsToBytes(123),
					DiskUsageInBytes:                    megsToBytes(456),
					TimeSpentInCPU:                      101 * time.Second,
					MemoryLimitInBytes:                  megsToBytes(789),
					DiskLimitInBytes:                    megsToBytes(1024),
					ContainerAgeInNanoseconds:           1004,
					AbsoluteCPUEntitlementInNanoseconds: 2004,
				},
			},

			"container-guid-without-index": executor.Metrics{
				MetricsConfig: executor.MetricsConfig{Guid: "metrics-guid-without-index"},
				ContainerMetrics: executor.ContainerMetrics{
					MemoryUsageInBytes:                  megsToBytes(1230),
					DiskUsageInBytes:                    4560,
					TimeSpentInCPU:                      105 * time.Second,
					MemoryLimitInBytes:                  megsToBytes(7890),
					DiskLimitInBytes:                    4096,
					ContainerAgeInNanoseconds:           1005,
					AbsoluteCPUEntitlementInNanoseconds: 2005,
				},
			},
			"container-guid-with-index": executor.Metrics{
				MetricsConfig: executor.MetricsConfig{Guid: "metrics-guid-with-index", Index: 1},
				ContainerMetrics: executor.ContainerMetrics{
					MemoryUsageInBytes:                  megsToBytes(3210),
					DiskUsageInBytes:                    6540,
					TimeSpentInCPU:                      110 * time.Second,
					MemoryLimitInBytes:                  megsToBytes(9870),
					DiskLimitInBytes:                    512,
					ContainerAgeInNanoseconds:           1006,
					AbsoluteCPUEntitlementInNanoseconds: 2006,
				}},
			"container-guid-without-preloaded-rootfs": executor.Metrics{
				MetricsConfig: executor.MetricsConfig{Guid: "metrics-guid-without-preloaded-rootfs"},
				ContainerMetrics: executor.ContainerMetrics{
					MemoryUsageInBytes:                  megsToBytes(3450),
					DiskUsageInBytes:                    4560,
					TimeSpentInCPU:                      110 * time.Second,
					MemoryLimitInBytes:                  megsToBytes(6780),
					DiskLimitInBytes:                    2048,
					ContainerAgeInNanoseconds:           1007,
					AbsoluteCPUEntitlementInNanoseconds: 2007,
				},
			},
		}

		metricsResultsChan <- map[string]executor.Metrics{
			"container-guid-without-metrics-guid": executor.Metrics{
				MetricsConfig: executor.MetricsConfig{Guid: ""},
				ContainerMetrics: executor.ContainerMetrics{
					MemoryUsageInBytes:                  megsToBytes(123),
					DiskUsageInBytes:                    megsToBytes(456),
					TimeSpentInCPU:                      100 * time.Second,
					MemoryLimitInBytes:                  megsToBytes(789),
					DiskLimitInBytes:                    megsToBytes(1024),
					ContainerAgeInNanoseconds:           1008,
					AbsoluteCPUEntitlementInNanoseconds: 2008,
				},
			},

			"container-guid-without-index": executor.Metrics{
				MetricsConfig: executor.MetricsConfig{Guid: "metrics-guid-without-index"},
				ContainerMetrics: executor.ContainerMetrics{
					MemoryUsageInBytes:                  megsToBytes(12300),
					DiskUsageInBytes:                    45600,
					TimeSpentInCPU:                      107 * time.Second,
					MemoryLimitInBytes:                  megsToBytes(7890),
					DiskLimitInBytes:                    234,
					ContainerAgeInNanoseconds:           1009,
					AbsoluteCPUEntitlementInNanoseconds: 2009,
				},
			},
			"container-guid-with-index": executor.Metrics{
				MetricsConfig: executor.MetricsConfig{Guid: "metrics-guid-with-index", Index: 1},
				ContainerMetrics: executor.ContainerMetrics{
					MemoryUsageInBytes:                  megsToBytes(32100),
					DiskUsageInBytes:                    65400,
					TimeSpentInCPU:                      112 * time.Second,
					MemoryLimitInBytes:                  megsToBytes(98700),
					DiskLimitInBytes:                    43200,
					ContainerAgeInNanoseconds:           1010,
					AbsoluteCPUEntitlementInNanoseconds: 2010,
				},
			},
			"container-guid-without-preloaded-rootfs": executor.Metrics{
				MetricsConfig: executor.MetricsConfig{Guid: "metrics-guid-without-preloaded-rootfs"},
				ContainerMetrics: executor.ContainerMetrics{
					MemoryUsageInBytes:                  megsToBytes(34500),
					DiskUsageInBytes:                    45600,
					TimeSpentInCPU:                      112 * time.Second,
					MemoryLimitInBytes:                  megsToBytes(6780),
					DiskLimitInBytes:                    2048,
					ContainerAgeInNanoseconds:           1011,
					AbsoluteCPUEntitlementInNanoseconds: 2011,
				},
			},
		}
	}

	sendContainers := func() {
		listContainersChan <- []executor.Container{
			{Guid: "container-guid-without-index"},
			{Guid: "container-guid-with-index"},
			{Guid: "container-guid-without-preloaded-rootfs"},
			{Guid: "container-guid-without-metrics-guid"},
		}
		listContainersChan <- []executor.Container{
			{Guid: "container-guid-without-index"},
			{Guid: "container-guid-with-index"},
			{Guid: "container-guid-without-preloaded-rootfs"},
			{Guid: "container-guid-without-metrics-guid"},
		}
		listContainersChan <- []executor.Container{
			{Guid: "container-guid-without-index"},
			{Guid: "container-guid-with-index"},
			{Guid: "container-guid-without-preloaded-rootfs"},
			{Guid: "container-guid-without-metrics-guid"},
		}
	}

	createEventContainerMetric := func(metrics executor.Metrics, cpuPercentage float64) logging.ContainerMetric {
		instanceIndex := int32(metrics.MetricsConfig.Index)
		return logging.ContainerMetric{
			ApplicationId:          metrics.MetricsConfig.Guid,
			InstanceIndex:          instanceIndex,
			CpuPercentage:          cpuPercentage,
			MemoryBytes:            metrics.ContainerMetrics.MemoryUsageInBytes,
			DiskBytes:              metrics.ContainerMetrics.DiskUsageInBytes,
			MemoryBytesQuota:       metrics.ContainerMetrics.MemoryLimitInBytes,
			DiskBytesQuota:         metrics.ContainerMetrics.DiskLimitInBytes,
			AbsoluteCPUUsage:       uint64(metrics.TimeSpentInCPU),
			AbsoluteCPUEntitlement: metrics.AbsoluteCPUEntitlementInNanoseconds,
			ContainerAge:           metrics.ContainerAgeInNanoseconds,
		}
	}

	type cpuUsage struct {
		applicationID       string
		instanceIndex       int32
		absoluteUsage       uint64
		absoluteEntitlement uint64
		containerAge        uint64
	}

	BeforeEach(func() {
		logger = lagertest.NewTestLogger("test")

		interval = 10 * time.Second
		fakeClock = fakeclock.NewFakeClock(time.Now())
		fakeExecutorClient = new(efakes.FakeClient)
		fakeMetronClient = new(mfakes.FakeIngressClient)

		metricsResultsChan = make(chan map[string]executor.Metrics, 10)
		listContainersChan = make(chan []executor.Container, 10)

		fakeExecutorClient.GetBulkMetricsStub = func(lager.Logger) (map[string]executor.Metrics, error) {

			result, ok := <-metricsResultsChan
			if !ok || result == nil {
				return nil, errors.New("closed")
			}
			return result, nil
		}

		fakeExecutorClient.ListContainersStub = func(lager.Logger) ([]executor.Container, error) {
			result, ok := <-listContainersChan
			if !ok || result == nil {
				return nil, errors.New("closed")
			}
			return result, nil
		}

		enableContainerProxy = false
		proxyMemoryAllocationMB = 5
	})

	JustBeforeEach(func() {
		reporter = containermetrics.NewStatsReporter(logger, interval, fakeClock, enableContainerProxy, proxyMemoryAllocationMB, fakeExecutorClient, fakeMetronClient)
		process = ifrit.Invoke(reporter)
	})

	AfterEach(func() {
		close(metricsResultsChan)
		close(listContainersChan)
		ginkgomon.Interrupt(process)
	})

	sentMetrics := func() []logging.ContainerMetric {
		evs := []logging.ContainerMetric{}
		for i := 0; i < fakeMetronClient.SendAppMetricsCallCount(); i++ {
			evs = append(evs, fakeMetronClient.SendAppMetricsArgsForCall(i))
		}
		return evs
	}

	sentCPUUsage := func() []cpuUsage {
		usage := []cpuUsage{}

		for i := 0; i < fakeMetronClient.SendAppMetricsCallCount(); i++ {
			metrics := fakeMetronClient.SendAppMetricsArgsForCall(i)
			usage = append(usage, cpuUsage{
				applicationID:       metrics.ApplicationId,
				instanceIndex:       metrics.InstanceIndex,
				absoluteUsage:       metrics.AbsoluteCPUUsage,
				absoluteEntitlement: metrics.AbsoluteCPUEntitlement,
				containerAge:        metrics.ContainerAge,
			})
		}
		return usage
	}

	Context("when the interval elapses", func() {
		JustBeforeEach(func() {
			sendContainers()
			sendResults()

			fakeClock.WaitForWatcherAndIncrement(interval)
			Eventually(fakeExecutorClient.GetBulkMetricsCallCount).Should(Equal(1))
		})

		It("emits memory and disk usage for each container, but no CPU", func() {
			Eventually(sentMetrics).Should(ConsistOf([]logging.ContainerMetric{
				createEventContainerMetric(executor.Metrics{
					MetricsConfig: executor.MetricsConfig{Guid: "metrics-guid-without-index"},
					ContainerMetrics: executor.ContainerMetrics{
						MemoryUsageInBytes:                  megsToBytes(123),
						DiskUsageInBytes:                    megsToBytes(456),
						MemoryLimitInBytes:                  megsToBytes(789),
						DiskLimitInBytes:                    megsToBytes(1024),
						TimeSpentInCPU:                      100000000000,
						AbsoluteCPUEntitlementInNanoseconds: 2001,
						ContainerAgeInNanoseconds:           1001,
					},
				},
					0.0),
				createEventContainerMetric(executor.Metrics{
					MetricsConfig: executor.MetricsConfig{Guid: "metrics-guid-with-index", Index: 1},
					ContainerMetrics: executor.ContainerMetrics{
						MemoryUsageInBytes:                  megsToBytes(321),
						DiskUsageInBytes:                    megsToBytes(654),
						MemoryLimitInBytes:                  megsToBytes(987),
						DiskLimitInBytes:                    megsToBytes(2048),
						TimeSpentInCPU:                      100000000000,
						AbsoluteCPUEntitlementInNanoseconds: 2002,
						ContainerAgeInNanoseconds:           1002,
					},
				},
					0.0),
				createEventContainerMetric(executor.Metrics{
					MetricsConfig: executor.MetricsConfig{Guid: "metrics-guid-without-preloaded-rootfs"},
					ContainerMetrics: executor.ContainerMetrics{
						MemoryUsageInBytes:                  megsToBytes(345),
						DiskUsageInBytes:                    megsToBytes(456),
						MemoryLimitInBytes:                  megsToBytes(678),
						DiskLimitInBytes:                    megsToBytes(2048),
						TimeSpentInCPU:                      100000000000,
						AbsoluteCPUEntitlementInNanoseconds: 2003,
						ContainerAgeInNanoseconds:           1003,
					},
				},
					0.0),
			}))
		})

		It("emits container cpu usage for each container", func() {
			Eventually(sentCPUUsage).Should(ConsistOf(
				cpuUsage{applicationID: "metrics-guid-without-index", absoluteUsage: uint64((100 * time.Second).Nanoseconds()), absoluteEntitlement: 2001, containerAge: 1001},
				cpuUsage{applicationID: "metrics-guid-with-index", instanceIndex: 1, absoluteUsage: uint64((100 * time.Second).Nanoseconds()), absoluteEntitlement: 2002, containerAge: 1002},
				cpuUsage{applicationID: "metrics-guid-without-preloaded-rootfs", absoluteUsage: uint64((100 * time.Second).Nanoseconds()), absoluteEntitlement: 2003, containerAge: 1003},
			))
		})

		Context("when containers EnableContainerProxy is true", func() {
			BeforeEach(func() {

				listContainersChan <- []executor.Container{
					{
						Guid:        "container-guid-without-index",
						MemoryLimit: megsToBytes(200 + proxyMemoryAllocationMB),
						RunInfo: executor.RunInfo{
							EnableContainerProxy: true,
							MetricsConfig:        executor.MetricsConfig{Guid: "metrics-guid-without-index"},
						},
					},
					{
						Guid:        "container-guid-with-index",
						MemoryLimit: megsToBytes(400 + proxyMemoryAllocationMB),
						RunInfo: executor.RunInfo{
							EnableContainerProxy: true,
							MetricsConfig:        executor.MetricsConfig{Guid: "metrics-guid-with-index", Index: 1},
						},
					},
					{
						Guid:        "container-guid-without-preloaded-rootfs",
						MemoryLimit: megsToBytes(200),
						RunInfo: executor.RunInfo{
							EnableContainerProxy: false,
							MetricsConfig:        executor.MetricsConfig{Guid: "metrics-guid-without-preloaded-rootfs"},
						},
					},
				}
			})

			It("does not change the memory usage reported by garden", func() {
				appMemory := megsToBytes(123)

				expectedMemoryUsageWithoutIndex := float64(appMemory)

				Eventually(sentMetrics).Should(ContainElement(
					createEventContainerMetric(executor.Metrics{
						MetricsConfig: executor.MetricsConfig{Guid: "metrics-guid-without-index"},
						ContainerMetrics: executor.ContainerMetrics{
							MemoryUsageInBytes:                  uint64(expectedMemoryUsageWithoutIndex),
							DiskUsageInBytes:                    megsToBytes(456),
							MemoryLimitInBytes:                  megsToBytes(789),
							DiskLimitInBytes:                    megsToBytes(1024),
							TimeSpentInCPU:                      100000000000,
							AbsoluteCPUEntitlementInNanoseconds: 2001,
							ContainerAgeInNanoseconds:           1001,
						},
					},
						0.0),
				))
			})

			Context("when enableContainerProxy is set", func() {
				BeforeEach(func() {
					enableContainerProxy = true
				})

				It("emits a rescaled memory usage based on the additional memory allocation for the proxy", func() {
					appMemory := megsToBytes(123)

					expectedMemoryUsageWithoutIndex := float64(appMemory) * 200.0 / (200.0 + float64(proxyMemoryAllocationMB))

					Eventually(sentMetrics).Should(ContainElement(
						createEventContainerMetric(executor.Metrics{
							MetricsConfig: executor.MetricsConfig{Guid: "metrics-guid-without-index"},
							ContainerMetrics: executor.ContainerMetrics{
								MemoryUsageInBytes:                  uint64(expectedMemoryUsageWithoutIndex),
								DiskUsageInBytes:                    megsToBytes(456),
								MemoryLimitInBytes:                  megsToBytes(784),
								DiskLimitInBytes:                    megsToBytes(1024),
								TimeSpentInCPU:                      100000000000,
								AbsoluteCPUEntitlementInNanoseconds: 2001,
								ContainerAgeInNanoseconds:           1001,
							},
						},
							0.0),
					))
				})

				Context("when there is a container without preloaded rootfs", func() {
					It("should emit memory usage that is not rescaled", func() {
						Eventually(sentMetrics).Should(ContainElement(
							createEventContainerMetric(executor.Metrics{
								MetricsConfig: executor.MetricsConfig{Guid: "metrics-guid-without-preloaded-rootfs"},
								ContainerMetrics: executor.ContainerMetrics{
									MemoryUsageInBytes:                  megsToBytes(345),
									DiskUsageInBytes:                    megsToBytes(456),
									MemoryLimitInBytes:                  megsToBytes(678),
									DiskLimitInBytes:                    megsToBytes(2048),
									TimeSpentInCPU:                      100000000000,
									AbsoluteCPUEntitlementInNanoseconds: 2003,
									ContainerAgeInNanoseconds:           1003,
								},
							},
								0.0),
						))
					})
				})
			})
		})

		It("does not emit anything for containers with no metrics guid", func() {
			Consistently(sentMetrics).ShouldNot(ContainElement(WithTransform(func(m logging.ContainerMetric) string {
				return m.ApplicationId
			}, BeEmpty())))
			Consistently(sentCPUUsage).ShouldNot(ContainElement(WithTransform(func(m cpuUsage) string {
				return m.applicationID
			}, BeEmpty())))
		})

		Context("and the interval elapses again", func() {
			JustBeforeEach(func() {
				fakeClock.WaitForWatcherAndIncrement(interval)
				Eventually(fakeExecutorClient.GetBulkMetricsCallCount).Should(Equal(2))
			})

			It("emits the new memory and disk usage, and the computed CPU percent", func() {
				Eventually(sentMetrics).Should(ContainElement(
					createEventContainerMetric(executor.Metrics{
						MetricsConfig: executor.MetricsConfig{Guid: "metrics-guid-without-index"},
						ContainerMetrics: executor.ContainerMetrics{
							MemoryUsageInBytes:                  megsToBytes(1230),
							DiskUsageInBytes:                    4560,
							MemoryLimitInBytes:                  megsToBytes(7890),
							DiskLimitInBytes:                    4096,
							TimeSpentInCPU:                      105000000000,
							AbsoluteCPUEntitlementInNanoseconds: 2005,
							ContainerAgeInNanoseconds:           1005,
						},
					}, 50.0)))
				Eventually(sentMetrics).Should(ContainElement(
					createEventContainerMetric(executor.Metrics{
						MetricsConfig: executor.MetricsConfig{Guid: "metrics-guid-with-index", Index: 1},
						ContainerMetrics: executor.ContainerMetrics{
							MemoryUsageInBytes:                  megsToBytes(3210),
							DiskUsageInBytes:                    6540,
							MemoryLimitInBytes:                  megsToBytes(9870),
							DiskLimitInBytes:                    512,
							TimeSpentInCPU:                      110000000000,
							AbsoluteCPUEntitlementInNanoseconds: 2006,
							ContainerAgeInNanoseconds:           1006,
						},
					},
						100.0)))
			})

			Context("Metrics", func() {
				It("returns the cached metrics last emitted", func() {
					containerMetrics := reporter.Metrics()
					Expect(containerMetrics).To(HaveLen(4))
					Expect(containerMetrics).To(HaveKeyWithValue("container-guid-without-index", &containermetrics.CachedContainerMetrics{
						MetricGUID:       "metrics-guid-without-index",
						CPUUsageFraction: 0.5,
						MemoryUsageBytes: megsToBytes(1230),
						DiskUsageBytes:   4560,
						MemoryQuotaBytes: megsToBytes(7890),
						DiskQuotaBytes:   4096,
					}))
					Expect(containerMetrics).To(HaveKeyWithValue("container-guid-with-index", &containermetrics.CachedContainerMetrics{
						MetricGUID:       "metrics-guid-with-index",
						CPUUsageFraction: 1.0,
						MemoryUsageBytes: megsToBytes(3210),
						DiskUsageBytes:   6540,
						MemoryQuotaBytes: megsToBytes(9870),
						DiskQuotaBytes:   512,
					}))
					Expect(containerMetrics).To(HaveKeyWithValue("container-guid-without-metrics-guid", &containermetrics.CachedContainerMetrics{
						MetricGUID:       "",
						CPUUsageFraction: 0.1,
						MemoryUsageBytes: megsToBytes(123),
						DiskUsageBytes:   megsToBytes(456),
						MemoryQuotaBytes: megsToBytes(789),
						DiskQuotaBytes:   megsToBytes(1024),
					}))
					Expect(containerMetrics).To(HaveKeyWithValue("container-guid-without-preloaded-rootfs", &containermetrics.CachedContainerMetrics{
						MetricGUID:       "metrics-guid-without-preloaded-rootfs",
						CPUUsageFraction: 1.0,
						MemoryUsageBytes: megsToBytes(3450),
						DiskUsageBytes:   4560,
						MemoryQuotaBytes: megsToBytes(6780),
						DiskQuotaBytes:   2048,
					}))
				})
			})

			Context("and the interval elapses again", func() {
				JustBeforeEach(func() {
					fakeClock.WaitForWatcherAndIncrement(interval)
					Eventually(fakeExecutorClient.GetBulkMetricsCallCount).Should(Equal(3))
				})

				It("emits the new memory and disk usage, and the computed CPU percent", func() {
					Eventually(sentMetrics).Should(ContainElement(
						createEventContainerMetric(executor.Metrics{
							MetricsConfig: executor.MetricsConfig{Guid: "metrics-guid-without-index"},
							ContainerMetrics: executor.ContainerMetrics{
								MemoryUsageInBytes:                  megsToBytes(12300),
								DiskUsageInBytes:                    45600,
								MemoryLimitInBytes:                  megsToBytes(7890),
								DiskLimitInBytes:                    234,
								TimeSpentInCPU:                      107000000000,
								AbsoluteCPUEntitlementInNanoseconds: 2009,
								ContainerAgeInNanoseconds:           1009,
							}},
							20.0),
					))
					Eventually(sentMetrics).Should(ContainElement(
						createEventContainerMetric(executor.Metrics{
							MetricsConfig: executor.MetricsConfig{Guid: "metrics-guid-with-index", Index: 1},
							ContainerMetrics: executor.ContainerMetrics{
								MemoryUsageInBytes:                  megsToBytes(32100),
								DiskUsageInBytes:                    65400,
								MemoryLimitInBytes:                  megsToBytes(98700),
								DiskLimitInBytes:                    43200,
								TimeSpentInCPU:                      112000000000,
								AbsoluteCPUEntitlementInNanoseconds: 2010,
								ContainerAgeInNanoseconds:           1010,
							},
						},
							20.0)))
				})
			})
		})
	})

	Context("when get all metrics fails", func() {
		JustBeforeEach(func() {
			metricsResultsChan <- nil

			fakeClock.Increment(interval)
			Eventually(fakeExecutorClient.GetBulkMetricsCallCount).Should(Equal(1))
		})

		It("does not blow up", func() {
			Consistently(process.Wait()).ShouldNot(Receive())
		})

		Context("and the interval elapses again, and it works that time", func() {
			JustBeforeEach(func() {
				sendContainers()
				sendResults()
				fakeClock.Increment(interval)
				Eventually(fakeExecutorClient.GetBulkMetricsCallCount).Should(Equal(2))
			})

			It("processes the containers happily", func() {
				Eventually(sentMetrics).Should(ContainElement(
					createEventContainerMetric(executor.Metrics{
						MetricsConfig: executor.MetricsConfig{Guid: "metrics-guid-without-index", Index: 0},
						ContainerMetrics: executor.ContainerMetrics{
							MemoryUsageInBytes:                  megsToBytes(123),
							DiskUsageInBytes:                    megsToBytes(456),
							MemoryLimitInBytes:                  megsToBytes(789),
							DiskLimitInBytes:                    megsToBytes(1024),
							TimeSpentInCPU:                      100 * time.Second,
							AbsoluteCPUEntitlementInNanoseconds: 2001,
							ContainerAgeInNanoseconds:           1001,
						}},
						0.0),
				))
			})
		})
	})

	Context("when a container is no longer present", func() {
		metrics := func(metricsGuid string, index int, cpuTime time.Duration) executor.Metrics {
			return executor.Metrics{
				MetricsConfig: executor.MetricsConfig{
					Guid:  metricsGuid,
					Index: index,
				},
				ContainerMetrics: executor.ContainerMetrics{
					TimeSpentInCPU: cpuTime,
				},
			}
		}

		waitForMetrics := func(id string, idx int, instance int32, cpu float64, cpuTime time.Duration) {
			EventuallyWithOffset(1, fakeMetronClient.SendAppMetricsCallCount).Should(Equal(idx + 1))
			ExpectWithOffset(1, fakeMetronClient.SendAppMetricsArgsForCall(idx)).To(Equal(
				createEventContainerMetric(
					executor.Metrics{
						MetricsConfig: executor.MetricsConfig{Guid: id, Index: int(instance)},
						ContainerMetrics: executor.ContainerMetrics{
							TimeSpentInCPU: cpuTime,
						},
					},
					cpu,
				),
			))
		}

		It("only remembers the previous metrics", func() {
			listContainersChan <- []executor.Container{{Guid: "container-guid-0"}}
			metricsResultsChan <- map[string]executor.Metrics{
				"container-guid-0": metrics("metrics-guid-0", 0, 10*time.Second),
			}

			fakeClock.WaitForWatcherAndIncrement(interval)

			// previousCPUInfos is empty so we emit 0.0
			waitForMetrics("metrics-guid-0", 0, 0, 0, 10*time.Second)

			By("getting more metrics from garden")
			listContainersChan <- []executor.Container{{Guid: "container-guid-0"}}
			metricsResultsChan <- map[string]executor.Metrics{
				"container-guid-0": metrics("metrics-guid-0", 0, 20*time.Second),
			}

			fakeClock.WaitForWatcherAndIncrement(interval)

			waitForMetrics("metrics-guid-0", 1, 0, 100, 20*time.Second)

			By("losing a container")
			listContainersChan <- []executor.Container{}
			metricsResultsChan <- map[string]executor.Metrics{}

			fakeClock.WaitForWatcherAndIncrement(interval)

			By("finding the container again")
			listContainersChan <- []executor.Container{{Guid: "container-guid-0"}, {Guid: "container-guid-1"}}
			metricsResultsChan <- map[string]executor.Metrics{
				"container-guid-0": metrics("metrics-guid-0", 0, 40*time.Second),
			}

			fakeClock.WaitForWatcherAndIncrement(interval)

			// previousCPUInfos is empty so we emit 0.0
			waitForMetrics("metrics-guid-0", 2, 0, 0, 40*time.Second)
		})
	})
})
