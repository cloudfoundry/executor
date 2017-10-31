package containermetrics_test

import (
	"errors"
	"time"

	"code.cloudfoundry.org/bbs/models"
	"code.cloudfoundry.org/clock/fakeclock"
	mfakes "code.cloudfoundry.org/diego-logging-client/testhelpers"
	"code.cloudfoundry.org/executor"
	"code.cloudfoundry.org/executor/containermetrics"
	efakes "code.cloudfoundry.org/executor/fakes"
	"code.cloudfoundry.org/lager"
	"code.cloudfoundry.org/lager/lagertest"
	msfake "github.com/cloudfoundry/dropsonde/metric_sender/fake"
	"github.com/cloudfoundry/sonde-go/events"
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
		fakeMetronClient   *mfakes.FakeIngressClient

		metricsResultsChan chan map[string]executor.Metrics
		listContainersChan chan []executor.Container
		process            ifrit.Process

		enableContainerProxy       bool
		additionalMemoryAllocation int
	)

	sendResults := func() {
		metricsResultsChan <- map[string]executor.Metrics{
			"container-guid-without-index": executor.Metrics{
				MetricsConfig: executor.MetricsConfig{Guid: "metrics-guid-without-index"},
				ContainerMetrics: executor.ContainerMetrics{
					MemoryUsageInBytes: 123,
					DiskUsageInBytes:   456,
					TimeSpentInCPU:     100 * time.Second,
					MemoryLimitInBytes: 789,
					DiskLimitInBytes:   1024,
				},
			},
			"container-guid-with-index": executor.Metrics{
				MetricsConfig: executor.MetricsConfig{Guid: "metrics-guid-with-index", Index: 1},
				ContainerMetrics: executor.ContainerMetrics{
					MemoryUsageInBytes: 321,
					DiskUsageInBytes:   654,
					TimeSpentInCPU:     100 * time.Second,
					MemoryLimitInBytes: 987,
					DiskLimitInBytes:   2048,
				},
			},
			"container-guid-without-preloaded-rootfs": executor.Metrics{
				MetricsConfig: executor.MetricsConfig{Guid: "metrics-guid-without-preloaded-rootfs"},
				ContainerMetrics: executor.ContainerMetrics{
					MemoryUsageInBytes: 345,
					DiskUsageInBytes:   456,
					TimeSpentInCPU:     100 * time.Second,
					MemoryLimitInBytes: 678,
					DiskLimitInBytes:   2048,
				},
			},
		}

		metricsResultsChan <- map[string]executor.Metrics{
			"container-guid-without-index": executor.Metrics{
				MetricsConfig: executor.MetricsConfig{Guid: "metrics-guid-without-index"},
				ContainerMetrics: executor.ContainerMetrics{
					MemoryUsageInBytes: 1230,
					DiskUsageInBytes:   4560,
					TimeSpentInCPU:     105 * time.Second,
					MemoryLimitInBytes: 7890,
					DiskLimitInBytes:   4096,
				},
			},
			"container-guid-with-index": executor.Metrics{
				MetricsConfig: executor.MetricsConfig{Guid: "metrics-guid-with-index", Index: 1},
				ContainerMetrics: executor.ContainerMetrics{
					MemoryUsageInBytes: 3210,
					DiskUsageInBytes:   6540,
					TimeSpentInCPU:     110 * time.Second,
					MemoryLimitInBytes: 9870,
					DiskLimitInBytes:   512,
				}},
			"container-guid-without-preloaded-rootfs": executor.Metrics{
				MetricsConfig: executor.MetricsConfig{Guid: "metrics-guid-without-preloaded-rootfs"},
				ContainerMetrics: executor.ContainerMetrics{
					MemoryUsageInBytes: 3450,
					DiskUsageInBytes:   4560,
					TimeSpentInCPU:     110 * time.Second,
					MemoryLimitInBytes: 6780,
					DiskLimitInBytes:   2048,
				},
			},
		}

		metricsResultsChan <- map[string]executor.Metrics{
			"container-guid-without-index": executor.Metrics{
				MetricsConfig: executor.MetricsConfig{Guid: "metrics-guid-without-index"},
				ContainerMetrics: executor.ContainerMetrics{
					MemoryUsageInBytes: 12300,
					DiskUsageInBytes:   45600,
					TimeSpentInCPU:     107 * time.Second,
					MemoryLimitInBytes: 7890,
					DiskLimitInBytes:   234,
				},
			},
			"container-guid-with-index": executor.Metrics{
				MetricsConfig: executor.MetricsConfig{Guid: "metrics-guid-with-index", Index: 1},
				ContainerMetrics: executor.ContainerMetrics{
					MemoryUsageInBytes: 32100,
					DiskUsageInBytes:   65400,
					TimeSpentInCPU:     112 * time.Second,
					MemoryLimitInBytes: 98700,
					DiskLimitInBytes:   43200,
				},
			},
			"container-guid-without-preloaded-rootfs": executor.Metrics{
				MetricsConfig: executor.MetricsConfig{Guid: "metrics-guid-without-preloaded-rootfs"},
				ContainerMetrics: executor.ContainerMetrics{
					MemoryUsageInBytes: 34500,
					DiskUsageInBytes:   45600,
					TimeSpentInCPU:     112 * time.Second,
					MemoryLimitInBytes: 6780,
					DiskLimitInBytes:   2048,
				},
			},
		}
	}

	sendContainers := func() {
		listContainersChan <- []executor.Container{
			{Guid: "container-guid-without-index"}, {Guid: "container-guid-with-index"}, {Guid: "container-guid-without-preloaded-rootfs"},
		}
		listContainersChan <- []executor.Container{
			{Guid: "container-guid-without-index"}, {Guid: "container-guid-with-index"}, {Guid: "container-guid-without-preloaded-rootfs"},
		}
		listContainersChan <- []executor.Container{
			{Guid: "container-guid-without-index"}, {Guid: "container-guid-with-index"}, {Guid: "container-guid-without-preloaded-rootfs"},
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

	BeforeEach(func() {
		logger = lagertest.NewTestLogger("test")

		interval = 10 * time.Second
		fakeClock = fakeclock.NewFakeClock(time.Now())
		fakeExecutorClient = new(efakes.FakeClient)
		fakeMetronClient = new(mfakes.FakeIngressClient)

		fakeMetricSender = msfake.NewFakeMetricSender()

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
		additionalMemoryAllocation = 5
	})

	JustBeforeEach(func() {
		process = ifrit.Invoke(containermetrics.NewStatsReporter(logger, interval, fakeClock, enableContainerProxy, additionalMemoryAllocation, fakeExecutorClient, fakeMetronClient))
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
						MemoryUsageInBytes: 123,
						DiskUsageInBytes:   456,
						MemoryLimitInBytes: 789,
						DiskLimitInBytes:   1024,
					},
				},
					0.0),
				createEventContainerMetric(executor.Metrics{
					MetricsConfig: executor.MetricsConfig{Guid: "metrics-guid-with-index", Index: 1},
					ContainerMetrics: executor.ContainerMetrics{
						MemoryUsageInBytes: 321,
						DiskUsageInBytes:   654,
						MemoryLimitInBytes: 987,
						DiskLimitInBytes:   2048,
					},
				},
					0.0),
				createEventContainerMetric(executor.Metrics{
					MetricsConfig: executor.MetricsConfig{Guid: "metrics-guid-without-preloaded-rootfs"},
					ContainerMetrics: executor.ContainerMetrics{
						MemoryUsageInBytes: uint64(345),
						DiskUsageInBytes:   456,
						MemoryLimitInBytes: 678,
						DiskLimitInBytes:   2048,
					},
				},
					0.0),
			}))
		})

		Context("when enableContainerProxy is set", func() {
			BeforeEach(func() {
				enableContainerProxy = true

				listContainersChan <- []executor.Container{
					{Guid: "container-guid-without-index",
						Resource: executor.Resource{
							RootFSPath: models.PreloadedRootFSScheme,
						},
						MemoryLimit: uint64(200 + additionalMemoryAllocation),
						RunInfo: executor.RunInfo{
							MetricsConfig: executor.MetricsConfig{Guid: "metrics-guid-without-index"},
						},
					},
					{Guid: "container-guid-with-index",
						Resource: executor.Resource{
							RootFSPath: models.PreloadedRootFSScheme,
						},
						MemoryLimit: uint64(400 + additionalMemoryAllocation),
						RunInfo: executor.RunInfo{
							MetricsConfig: executor.MetricsConfig{Guid: "metrics-guid-with-index", Index: 1},
						},
					},
					{Guid: "container-guid-without-preloaded-rootfs",
						MemoryLimit: uint64(200),
						Resource: executor.Resource{
							RootFSPath: "unsupported-arbitrary://still-goes-through",
						},
						RunInfo: executor.RunInfo{
							MetricsConfig: executor.MetricsConfig{Guid: "metrics-guid-without-preloaded-rootfs"},
						},
					},
				}

			})

			It("emits a rescaled memory usage based on the additional memory allocation for the proxy", func() {
				expectedMemoryUsageWithoutIndex := float64(123) * float64(200) / float64(200+additionalMemoryAllocation)
				expectedMemoryUsageWithIndex := float64(321) * float64(400) / float64(400+additionalMemoryAllocation)

				Eventually(sentMetrics).Should(ContainElement(
					createEventContainerMetric(executor.Metrics{
						MetricsConfig: executor.MetricsConfig{Guid: "metrics-guid-without-index"},
						ContainerMetrics: executor.ContainerMetrics{
							MemoryUsageInBytes: uint64(expectedMemoryUsageWithoutIndex),
							DiskUsageInBytes:   456,
							MemoryLimitInBytes: 789,
							DiskLimitInBytes:   1024,
						},
					},
						0.0),
				))
				Eventually(sentMetrics).Should(ContainElement(
					createEventContainerMetric(executor.Metrics{
						MetricsConfig: executor.MetricsConfig{Guid: "metrics-guid-with-index", Index: 1},
						ContainerMetrics: executor.ContainerMetrics{
							MemoryUsageInBytes: uint64(expectedMemoryUsageWithIndex),
							DiskUsageInBytes:   654,
							MemoryLimitInBytes: 987,
							DiskLimitInBytes:   2048,
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
								MemoryUsageInBytes: uint64(345),
								DiskUsageInBytes:   456,
								MemoryLimitInBytes: 678,
								DiskLimitInBytes:   2048,
							},
						},
							0.0),
					))
				})
			})

		})

		It("does not emit anything for containers with no metrics guid", func() {
			Consistently(func() msfake.ContainerMetric {
				return fakeMetricSender.GetContainerMetric("")
			}).Should(BeZero())
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
							MemoryUsageInBytes: 1230,
							DiskUsageInBytes:   4560,
							MemoryLimitInBytes: 7890,
							DiskLimitInBytes:   4096,
						},
					}, 50.0)))
				Eventually(sentMetrics).Should(ContainElement(
					createEventContainerMetric(executor.Metrics{
						MetricsConfig: executor.MetricsConfig{Guid: "metrics-guid-with-index", Index: 1},
						ContainerMetrics: executor.ContainerMetrics{
							MemoryUsageInBytes: 3210,
							DiskUsageInBytes:   6540,
							MemoryLimitInBytes: 9870,
							DiskLimitInBytes:   512,
						},
					},
						100.0)))
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
								MemoryUsageInBytes: 12300,
								DiskUsageInBytes:   45600,
								MemoryLimitInBytes: 7890,
								DiskLimitInBytes:   234,
							}},
							20.0),
					))
					Eventually(sentMetrics).Should(ContainElement(
						createEventContainerMetric(executor.Metrics{
							MetricsConfig: executor.MetricsConfig{Guid: "metrics-guid-with-index", Index: 1},
							ContainerMetrics: executor.ContainerMetrics{
								MemoryUsageInBytes: 32100,
								DiskUsageInBytes:   65400,
								MemoryLimitInBytes: 98700,
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
							MemoryUsageInBytes: 123,
							DiskUsageInBytes:   456,
							MemoryLimitInBytes: 789,
							DiskLimitInBytes:   1024,
						}},
						0.0),
				))
				Eventually(sentMetrics).Should(ContainElement(
					createEventContainerMetric(executor.Metrics{
						MetricsConfig: executor.MetricsConfig{Guid: "metrics-guid-with-index", Index: 1},
						ContainerMetrics: executor.ContainerMetrics{
							MemoryUsageInBytes: 321,
							DiskUsageInBytes:   654,
							MemoryLimitInBytes: 987,
							DiskLimitInBytes:   2048,
						}},
						0.0),
				))
			})
		})
	})

	Context("when a container is no longer present", func() {
		metrics := func(metricsGuid string, index int, memoryUsage, diskUsage, memoryLimit, diskLimit uint64, cpuTime time.Duration) executor.Metrics {
			return executor.Metrics{
				MetricsConfig: executor.MetricsConfig{
					Guid:  metricsGuid,
					Index: index,
				},
				ContainerMetrics: executor.ContainerMetrics{
					MemoryUsageInBytes: memoryUsage,
					DiskUsageInBytes:   diskUsage,
					MemoryLimitInBytes: memoryLimit,
					DiskLimitInBytes:   diskLimit,
					TimeSpentInCPU:     cpuTime,
				},
			}
		}

		waitForMetrics := func(id string, instance int32, cpu float64, memoryUsage, diskUsage, memoryLimit, diskLimit uint64) {
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
		}

		// Pending test: This test was passing, but the functionality was not actually working.
		// When we discovered that the test setup does not mimic reality, we filed
		// a bug [#152337746], and made the test fail, hence it's pending until we get to that bug.
		PIt("only remembers the previous metrics", func() {
			listContainersChan <- []executor.Container{{Guid: "container-guid-0"}, {Guid: "container-guid-1"}}
			metricsResultsChan <- map[string]executor.Metrics{
				"container-guid-0": metrics("metrics-guid-0", 0, 128, 256, 512, 1024, 10*time.Second),
				"container-guid-1": metrics("metrics-guid-1", 1, 256, 512, 1024, 2048, 10*time.Second),
			}

			fakeClock.Increment(interval)

			waitForMetrics("metrics-guid-0", 0, 0, 128, 256, 512, 1024)
			waitForMetrics("metrics-guid-1", 1, 0, 256, 512, 1024, 2048)

			Expect(fakeMetronClient.SendAppMetricsCallCount).To(Equal(2))

			By("losing a container")
			listContainersChan <- []executor.Container{{Guid: "container-guid-0"}, {Guid: "container-guid-1"}}
			metricsResultsChan <- map[string]executor.Metrics{
				"container-guid-0": metrics("metrics-guid-0", 0, 256, 512, 1024, 2048, 30*time.Second),
			}

			fakeClock.Increment(interval)

			waitForMetrics("metrics-guid-0", 0, 200, 256, 512, 1024, 2048)
			waitForMetrics("metrics-guid-1", 1, 0, 256, 512, 1024, 2048)

			Expect(fakeMetronClient.SendAppMetricsCallCount).To(Equal(4))

			By("finding the container again")
			listContainersChan <- []executor.Container{{Guid: "container-guid-0"}, {Guid: "container-guid-1"}}
			metricsResultsChan <- map[string]executor.Metrics{
				"container-guid-0": metrics("metrics-guid-0", 0, 256, 512, 2, 3, 40*time.Second),
				"container-guid-1": metrics("metrics-guid-1", 1, 512, 1024, 3, 2, 10*time.Second),
			}

			fakeClock.Increment(interval)

			waitForMetrics("metrics-guid-0", 0, 100, 256, 512, 2, 3)
			waitForMetrics("metrics-guid-1", 1, 0, 512, 1024, 3, 2)
			Expect(fakeMetronClient.SendAppMetricsCallCount).To(Equal(6))
		})
	})
})
