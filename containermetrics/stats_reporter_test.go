package containermetrics_test

import (
	"errors"
	"time"

	"code.cloudfoundry.org/clock/fakeclock"
	mfakes "code.cloudfoundry.org/diego-logging-client/testhelpers"
	"code.cloudfoundry.org/executor"
	"code.cloudfoundry.org/executor/containermetrics"
	efakes "code.cloudfoundry.org/executor/fakes"
	"code.cloudfoundry.org/lager"
	"code.cloudfoundry.org/lager/lagertest"
	"github.com/cloudfoundry/sonde-go/events"
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

	createEventContainerMetric := func(metrics executor.Metrics, cpuPercentage float64) events.ContainerMetric {
		instanceIndex := int32(metrics.MetricsConfig.Index)
		return events.ContainerMetric{
			ApplicationId:    &metrics.MetricsConfig.Guid,
			InstanceIndex:    &instanceIndex,
			CpuPercentage:    &cpuPercentage,
			MemoryBytes:      &metrics.ContainerMetrics.MemoryUsageInBytes,
			DiskBytes:        &metrics.ContainerMetrics.DiskUsageInBytes,
			MemoryBytesQuota: &metrics.ContainerMetrics.MemoryLimitInBytes,
			DiskBytesQuota:   &metrics.ContainerMetrics.DiskLimitInBytes,
		}
	}

	type cpuUsage struct {
		applicationID       string
		instanceIndex       int
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

	sentMetrics := func() []events.ContainerMetric {
		evs := []events.ContainerMetric{}
		for i := 0; i < fakeMetronClient.SendAppMetricsCallCount(); i++ {
			evs = append(evs, *fakeMetronClient.SendAppMetricsArgsForCall(i))
		}
		return evs
	}

	sentCPUUsage := func() []cpuUsage {
		usage := []cpuUsage{}

		for i := 0; i < fakeMetronClient.SendCPUUsageCallCount(); i++ {
			applicationID, instanceIndex, absoluteUsage, absoluteEntitlement, containerAge := fakeMetronClient.SendCPUUsageArgsForCall(i)
			usage = append(usage, cpuUsage{
				applicationID:       applicationID,
				instanceIndex:       instanceIndex,
				absoluteUsage:       absoluteUsage,
				absoluteEntitlement: absoluteEntitlement,
				containerAge:        containerAge,
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
			Eventually(sentMetrics).Should(ConsistOf([]events.ContainerMetric{
				createEventContainerMetric(executor.Metrics{
					MetricsConfig: executor.MetricsConfig{Guid: "metrics-guid-without-index"},
					ContainerMetrics: executor.ContainerMetrics{
						MemoryUsageInBytes: megsToBytes(123),
						DiskUsageInBytes:   megsToBytes(456),
						MemoryLimitInBytes: megsToBytes(789),
						DiskLimitInBytes:   megsToBytes(1024),
					},
				},
					0.0),
				createEventContainerMetric(executor.Metrics{
					MetricsConfig: executor.MetricsConfig{Guid: "metrics-guid-with-index", Index: 1},
					ContainerMetrics: executor.ContainerMetrics{
						MemoryUsageInBytes: megsToBytes(321),
						DiskUsageInBytes:   megsToBytes(654),
						MemoryLimitInBytes: megsToBytes(987),
						DiskLimitInBytes:   megsToBytes(2048),
					},
				},
					0.0),
				createEventContainerMetric(executor.Metrics{
					MetricsConfig: executor.MetricsConfig{Guid: "metrics-guid-without-preloaded-rootfs"},
					ContainerMetrics: executor.ContainerMetrics{
						MemoryUsageInBytes: megsToBytes(345),
						DiskUsageInBytes:   megsToBytes(456),
						MemoryLimitInBytes: megsToBytes(678),
						DiskLimitInBytes:   megsToBytes(2048),
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
							MemoryUsageInBytes: uint64(expectedMemoryUsageWithoutIndex),
							DiskUsageInBytes:   megsToBytes(456),
							MemoryLimitInBytes: megsToBytes(789),
							DiskLimitInBytes:   megsToBytes(1024),
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
								MemoryUsageInBytes: uint64(expectedMemoryUsageWithoutIndex),
								DiskUsageInBytes:   megsToBytes(456),
								MemoryLimitInBytes: megsToBytes(784),
								DiskLimitInBytes:   megsToBytes(1024),
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
									MemoryUsageInBytes: megsToBytes(345),
									DiskUsageInBytes:   megsToBytes(456),
									MemoryLimitInBytes: megsToBytes(678),
									DiskLimitInBytes:   megsToBytes(2048),
								},
							},
								0.0),
						))
					})
				})
			})
		})

		It("does not emit anything for containers with no metrics guid", func() {
			Consistently(sentMetrics).ShouldNot(ContainElement(WithTransform(func(m events.ContainerMetric) string {
				return m.GetApplicationId()
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
							MemoryUsageInBytes: megsToBytes(1230),
							DiskUsageInBytes:   4560,
							MemoryLimitInBytes: megsToBytes(7890),
							DiskLimitInBytes:   4096,
						},
					}, 50.0)))
				Eventually(sentMetrics).Should(ContainElement(
					createEventContainerMetric(executor.Metrics{
						MetricsConfig: executor.MetricsConfig{Guid: "metrics-guid-with-index", Index: 1},
						ContainerMetrics: executor.ContainerMetrics{
							MemoryUsageInBytes: megsToBytes(3210),
							DiskUsageInBytes:   6540,
							MemoryLimitInBytes: megsToBytes(9870),
							DiskLimitInBytes:   512,
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
								MemoryUsageInBytes: megsToBytes(12300),
								DiskUsageInBytes:   45600,
								MemoryLimitInBytes: megsToBytes(7890),
								DiskLimitInBytes:   234,
							}},
							20.0),
					))
					Eventually(sentMetrics).Should(ContainElement(
						createEventContainerMetric(executor.Metrics{
							MetricsConfig: executor.MetricsConfig{Guid: "metrics-guid-with-index", Index: 1},
							ContainerMetrics: executor.ContainerMetrics{
								MemoryUsageInBytes: megsToBytes(32100),
								DiskUsageInBytes:   65400,
								MemoryLimitInBytes: megsToBytes(98700),
								DiskLimitInBytes:   43200,
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
							MemoryUsageInBytes: megsToBytes(123),
							DiskUsageInBytes:   megsToBytes(456),
							MemoryLimitInBytes: megsToBytes(789),
							DiskLimitInBytes:   megsToBytes(1024),
						}},
						0.0),
				))

				Eventually(sentCPUUsage).Should(ConsistOf(
					cpuUsage{applicationID: "metrics-guid-without-index", absoluteUsage: uint64((100 * time.Second).Nanoseconds()), absoluteEntitlement: 2001, containerAge: 1001},
					cpuUsage{applicationID: "metrics-guid-with-index", instanceIndex: 1, absoluteUsage: uint64((100 * time.Second).Nanoseconds()), absoluteEntitlement: 2002, containerAge: 1002},
					cpuUsage{applicationID: "metrics-guid-without-preloaded-rootfs", absoluteUsage: uint64((100 * time.Second).Nanoseconds()), absoluteEntitlement: 2003, containerAge: 1003},
				))
			})
		})
	})

	Context("when a container is no longer present", func() {
		metrics := func(metricsGuid string, index int, memoryUsage, diskUsage, memoryLimit, diskLimit uint64, cpuTime time.Duration, containerAge, absoluteCPUEntitlement uint64) executor.Metrics {
			return executor.Metrics{
				MetricsConfig: executor.MetricsConfig{
					Guid:  metricsGuid,
					Index: index,
				},
				ContainerMetrics: executor.ContainerMetrics{
					MemoryUsageInBytes:                  memoryUsage,
					DiskUsageInBytes:                    diskUsage,
					MemoryLimitInBytes:                  memoryLimit,
					DiskLimitInBytes:                    diskLimit,
					TimeSpentInCPU:                      cpuTime,
					ContainerAgeInNanoseconds:           containerAge,
					AbsoluteCPUEntitlementInNanoseconds: absoluteCPUEntitlement,
				},
			}
		}

		waitForMetrics := func(id string, instance int32, cpu float64, memoryUsage, diskUsage, memoryLimit, diskLimit uint64, containerAge, absoluteCPUUsage, absoluteCPUEntitlement uint64) {
			Eventually(sentMetrics).Should(ContainElement(
				createEventContainerMetric(executor.Metrics{
					MetricsConfig: executor.MetricsConfig{Guid: id, Index: int(instance)},
					ContainerMetrics: executor.ContainerMetrics{
						MemoryUsageInBytes: memoryUsage,
						DiskUsageInBytes:   diskUsage,
						MemoryLimitInBytes: memoryLimit,
						DiskLimitInBytes:   diskLimit,
					}},
					cpu),
			))
			Eventually(sentCPUUsage).Should(ContainElement(cpuUsage{applicationID: id, instanceIndex: int(instance), absoluteUsage: absoluteCPUUsage, absoluteEntitlement: absoluteCPUEntitlement, containerAge: containerAge}))
		}

		// Pending test: This test was passing, but the functionality was not actually working.
		// When we discovered that the test setup does not mimic reality, we filed
		// a bug [#152337746], and made the test fail, hence it's pending until we get to that bug.
		PIt("only remembers the previous metrics", func() {
			listContainersChan <- []executor.Container{{Guid: "container-guid-0"}, {Guid: "container-guid-1"}}
			metricsResultsChan <- map[string]executor.Metrics{
				"container-guid-0": metrics("metrics-guid-0", 0, 128, 256, 512, 1024, 10*time.Second, 1000, 1002),
				"container-guid-1": metrics("metrics-guid-1", 1, 256, 512, 1024, 2048, 10*time.Second, 2000, 2002),
			}

			fakeClock.Increment(interval)

			waitForMetrics("metrics-guid-0", 0, 0, 128, 256, 512, 1024, 1000, uint64((10 * time.Second).Nanoseconds()), 1002)
			waitForMetrics("metrics-guid-1", 1, 0, 256, 512, 1024, 2048, 2000, uint64((10 * time.Second).Nanoseconds()), 2002)

			Expect(fakeMetronClient.SendAppMetricsCallCount).To(Equal(2))

			By("losing a container")
			listContainersChan <- []executor.Container{{Guid: "container-guid-0"}, {Guid: "container-guid-1"}}
			metricsResultsChan <- map[string]executor.Metrics{
				"container-guid-0": metrics("metrics-guid-0", 0, 256, 512, 1024, 2048, 30*time.Second, 3000, 3002),
			}

			fakeClock.Increment(interval)

			waitForMetrics("metrics-guid-0", 0, 200, 256, 512, 1024, 2048, 3000, uint64((30 * time.Second).Nanoseconds()), 3002)
			waitForMetrics("metrics-guid-1", 1, 0, 256, 512, 1024, 2048, 2000, uint64((10 * time.Second).Nanoseconds()), 2002)

			Expect(fakeMetronClient.SendAppMetricsCallCount).To(Equal(4))
			Expect(fakeMetronClient.SendCPUUsageCallCount).To(Equal(4))

			By("finding the container again")
			listContainersChan <- []executor.Container{{Guid: "container-guid-0"}, {Guid: "container-guid-1"}}
			metricsResultsChan <- map[string]executor.Metrics{
				"container-guid-0": metrics("metrics-guid-0", 0, 256, 512, 2, 3, 40*time.Second, 4000, 4002),
				"container-guid-1": metrics("metrics-guid-1", 1, 512, 1024, 3, 2, 10*time.Second, 5000, 5002),
			}

			fakeClock.Increment(interval)

			waitForMetrics("metrics-guid-0", 0, 100, 256, 512, 2, 3, 4000, uint64((40 * time.Second).Nanoseconds()), 4002)
			waitForMetrics("metrics-guid-1", 1, 0, 512, 1024, 3, 2, 5000, uint64((10 * time.Second).Nanoseconds()), 5002)
			Expect(fakeMetronClient.SendAppMetricsCallCount).To(Equal(6))
			Expect(fakeMetronClient.SendCPUUsageCallCount).To(Equal(6))
		})
	})
})
