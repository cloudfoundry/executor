package containermetrics_test

import (
	"strconv"
	"time"

	"code.cloudfoundry.org/clock/fakeclock"
	logging "code.cloudfoundry.org/diego-logging-client"
	mfakes "code.cloudfoundry.org/diego-logging-client/testhelpers"
	"code.cloudfoundry.org/executor"
	"code.cloudfoundry.org/executor/containermetrics"
	efakes "code.cloudfoundry.org/executor/fakes"
	"code.cloudfoundry.org/lager/lagertest"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/ginkgomon"
)

func megsToBytes(mem int) uint64 {
	return uint64(mem * 1024 * 1024)
}

type cpuUsage struct {
	applicationID       string
	instanceIndex       int32
	absoluteUsage       uint64
	absoluteEntitlement uint64
	containerAge        uint64
}

var _ = Describe("StatsReporter", func() {
	var (
		logger *lagertest.TestLogger

		interval           time.Duration
		fakeClock          *fakeclock.FakeClock
		fakeExecutorClient *efakes.FakeClient
		fakeMetronClient   *mfakes.FakeIngressClient

		process ifrit.Process

		enableContainerProxy    bool
		proxyMemoryAllocationMB int
		reporter                *containermetrics.StatsReporter
	)

	BeforeEach(func() {
		logger = lagertest.NewTestLogger("test")

		interval = 10 * time.Second
		fakeClock = fakeclock.NewFakeClock(time.Now())
		fakeExecutorClient = new(efakes.FakeClient)
		fakeMetronClient = new(mfakes.FakeIngressClient)

		enableContainerProxy = false
		proxyMemoryAllocationMB = 5
	})

	JustBeforeEach(func() {
		reporter = containermetrics.NewStatsReporter(logger, interval, fakeClock, enableContainerProxy, proxyMemoryAllocationMB, fakeExecutorClient, fakeMetronClient)
		process = ifrit.Invoke(reporter)
		fakeClock.WaitForWatcherAndIncrement(interval)
		Eventually(fakeExecutorClient.GetBulkMetricsCallCount).Should(Equal(1))
	})

	AfterEach(func() {
		ginkgomon.Interrupt(process)
	})

	sentCPUUsage := func() []cpuUsage {
		usage := []cpuUsage{}

		for i := 0; i < fakeMetronClient.SendAppMetricsCallCount(); i++ {
			metrics := fakeMetronClient.SendAppMetricsArgsForCall(i)
			index, _ := strconv.Atoi(metrics.Tags["instance_id"])
			usage = append(usage, cpuUsage{
				applicationID:       metrics.Tags["source_id"],
				instanceIndex:       int32(index),
				absoluteUsage:       metrics.AbsoluteCPUUsage,
				absoluteEntitlement: metrics.AbsoluteCPUEntitlement,
				containerAge:        metrics.ContainerAge,
			})
		}
		return usage
	}

	sentMetrics := func() []logging.ContainerMetric {
		evs := []logging.ContainerMetric{}
		for i := 0; i < fakeMetronClient.SendAppMetricsCallCount(); i++ {
			evs = append(evs, fakeMetronClient.SendAppMetricsArgsForCall(i))
		}
		return evs
	}

	Context("when the interval elapses", func() {
		var metricsAtT0, metricsAtT10, metricsAtT20 map[string]executor.Metrics

		BeforeEach(func() {
			containers1 := []executor.Container{
				{Guid: "container-guid-without-index"},
				{Guid: "container-guid-with-index"},
				{Guid: "container-guid-without-preloaded-rootfs"},
				{Guid: "container-guid-without-source-id"},
				{Guid: "container-guid-without-age"},
			}
			containers2 := []executor.Container{
				{Guid: "container-guid-without-index"},
				{Guid: "container-guid-with-index"},
				{Guid: "container-guid-without-preloaded-rootfs"},
				{Guid: "container-guid-without-source-id"},
				{Guid: "container-guid-without-age"},
			}
			containers3 := []executor.Container{
				{Guid: "container-guid-without-index"},
				{Guid: "container-guid-with-index"},
				{Guid: "container-guid-without-preloaded-rootfs"},
				{Guid: "container-guid-without-source-id"},
				{Guid: "container-guid-without-age"},
			}

			fakeExecutorClient.ListContainersReturnsOnCall(0, containers1, nil)
			fakeExecutorClient.ListContainersReturnsOnCall(1, containers2, nil)
			fakeExecutorClient.ListContainersReturnsOnCall(2, containers3, nil)

			metricsAtT0 = map[string]executor.Metrics{
				"container-guid-without-source-id": executor.Metrics{
					MetricsConfig: executor.MetricsConfig{Tags: map[string]string{}},
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
					MetricsConfig: executor.MetricsConfig{Tags: map[string]string{"source_id": "source-id-without-index"}},
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
					MetricsConfig: executor.MetricsConfig{Tags: map[string]string{"source_id": "source-id-with-index", "instance_id": "1"}},
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
					MetricsConfig: executor.MetricsConfig{Tags: map[string]string{"source_id": "source-id-without-preloaded-rootfs"}},
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
				"container-guid-without-age": executor.Metrics{
					MetricsConfig: executor.MetricsConfig{Tags: map[string]string{"source_id": "source-id-without-age"}},
					ContainerMetrics: executor.ContainerMetrics{
						MemoryUsageInBytes:                  megsToBytes(123),
						DiskUsageInBytes:                    megsToBytes(456),
						TimeSpentInCPU:                      100 * time.Second,
						MemoryLimitInBytes:                  megsToBytes(789),
						DiskLimitInBytes:                    megsToBytes(1024),
						ContainerAgeInNanoseconds:           0,
						AbsoluteCPUEntitlementInNanoseconds: 2004,
					},
				},
			}

			metricsAtT10 = map[string]executor.Metrics{
				"container-guid-without-source-id": executor.Metrics{
					MetricsConfig: executor.MetricsConfig{Tags: map[string]string{}},
					ContainerMetrics: executor.ContainerMetrics{
						MemoryUsageInBytes:                  megsToBytes(123),
						DiskUsageInBytes:                    megsToBytes(456),
						TimeSpentInCPU:                      101 * time.Second,
						MemoryLimitInBytes:                  megsToBytes(789),
						DiskLimitInBytes:                    megsToBytes(1024),
						ContainerAgeInNanoseconds:           1000 + uint64(10*time.Second),
						AbsoluteCPUEntitlementInNanoseconds: 2000,
					},
				},

				"container-guid-without-index": executor.Metrics{
					MetricsConfig: executor.MetricsConfig{Tags: map[string]string{"source_id": "source-id-without-index"}},
					ContainerMetrics: executor.ContainerMetrics{
						MemoryUsageInBytes:                  megsToBytes(1230),
						DiskUsageInBytes:                    4560,
						TimeSpentInCPU:                      105 * time.Second,
						MemoryLimitInBytes:                  megsToBytes(7890),
						DiskLimitInBytes:                    4096,
						ContainerAgeInNanoseconds:           1001 + uint64(10*time.Second),
						AbsoluteCPUEntitlementInNanoseconds: 2001,
					},
				},
				"container-guid-with-index": executor.Metrics{
					MetricsConfig: executor.MetricsConfig{Tags: map[string]string{"source_id": "source-id-with-index", "instance_id": "1"}},
					ContainerMetrics: executor.ContainerMetrics{
						MemoryUsageInBytes:                  megsToBytes(3210),
						DiskUsageInBytes:                    6540,
						TimeSpentInCPU:                      110 * time.Second,
						MemoryLimitInBytes:                  megsToBytes(9870),
						DiskLimitInBytes:                    512,
						ContainerAgeInNanoseconds:           1002 + uint64(10*time.Second),
						AbsoluteCPUEntitlementInNanoseconds: 2002,
					}},
				"container-guid-without-preloaded-rootfs": executor.Metrics{
					MetricsConfig: executor.MetricsConfig{Tags: map[string]string{"source_id": "source-id-without-preloaded-rootfs"}},
					ContainerMetrics: executor.ContainerMetrics{
						MemoryUsageInBytes:                  megsToBytes(3450),
						DiskUsageInBytes:                    4560,
						TimeSpentInCPU:                      110 * time.Second,
						MemoryLimitInBytes:                  megsToBytes(6780),
						DiskLimitInBytes:                    2048,
						ContainerAgeInNanoseconds:           1003 + uint64(10*time.Second),
						AbsoluteCPUEntitlementInNanoseconds: 2003,
					},
				},
				"container-guid-without-age": executor.Metrics{
					MetricsConfig: executor.MetricsConfig{Tags: map[string]string{"source_id": "source-id-without-age"}},
					ContainerMetrics: executor.ContainerMetrics{
						MemoryUsageInBytes:                  megsToBytes(123),
						DiskUsageInBytes:                    megsToBytes(456),
						TimeSpentInCPU:                      105 * time.Second,
						MemoryLimitInBytes:                  megsToBytes(789),
						DiskLimitInBytes:                    megsToBytes(1024),
						ContainerAgeInNanoseconds:           0,
						AbsoluteCPUEntitlementInNanoseconds: 2004,
					},
				},
			}

			metricsAtT20 = map[string]executor.Metrics{
				"container-guid-without-source-id": executor.Metrics{
					MetricsConfig: executor.MetricsConfig{Tags: map[string]string{}},
					ContainerMetrics: executor.ContainerMetrics{
						MemoryUsageInBytes:                  megsToBytes(123),
						DiskUsageInBytes:                    megsToBytes(456),
						TimeSpentInCPU:                      100 * time.Second,
						MemoryLimitInBytes:                  megsToBytes(789),
						DiskLimitInBytes:                    megsToBytes(1024),
						ContainerAgeInNanoseconds:           1000 + uint64(20*time.Second),
						AbsoluteCPUEntitlementInNanoseconds: 2000,
					},
				},

				"container-guid-without-index": executor.Metrics{
					MetricsConfig: executor.MetricsConfig{Tags: map[string]string{"source_id": "source-id-without-index"}},
					ContainerMetrics: executor.ContainerMetrics{
						MemoryUsageInBytes:                  megsToBytes(12300),
						DiskUsageInBytes:                    45600,
						TimeSpentInCPU:                      107 * time.Second,
						MemoryLimitInBytes:                  megsToBytes(7890),
						DiskLimitInBytes:                    234,
						ContainerAgeInNanoseconds:           1001 + uint64(20*time.Second),
						AbsoluteCPUEntitlementInNanoseconds: 2001,
					},
				},
				"container-guid-with-index": executor.Metrics{
					MetricsConfig: executor.MetricsConfig{Tags: map[string]string{"source_id": "source-id-with-index", "instance_id": "1"}},
					ContainerMetrics: executor.ContainerMetrics{
						MemoryUsageInBytes:                  megsToBytes(32100),
						DiskUsageInBytes:                    65400,
						TimeSpentInCPU:                      112 * time.Second,
						MemoryLimitInBytes:                  megsToBytes(98700),
						DiskLimitInBytes:                    43200,
						ContainerAgeInNanoseconds:           1002 + uint64(20*time.Second),
						AbsoluteCPUEntitlementInNanoseconds: 2002,
					},
				},
				"container-guid-without-preloaded-rootfs": executor.Metrics{
					MetricsConfig: executor.MetricsConfig{Tags: map[string]string{"source_id": "source-id-without-preloaded-rootfs"}},
					ContainerMetrics: executor.ContainerMetrics{
						MemoryUsageInBytes:                  megsToBytes(34500),
						DiskUsageInBytes:                    45600,
						TimeSpentInCPU:                      112 * time.Second,
						MemoryLimitInBytes:                  megsToBytes(6780),
						DiskLimitInBytes:                    2048,
						ContainerAgeInNanoseconds:           1003 + uint64(20*time.Second),
						AbsoluteCPUEntitlementInNanoseconds: 2003,
					},
				},
				"container-guid-without-age": executor.Metrics{
					MetricsConfig: executor.MetricsConfig{Tags: map[string]string{"source_id": "source-id-without-age"}},
					ContainerMetrics: executor.ContainerMetrics{
						MemoryUsageInBytes:                  megsToBytes(123),
						DiskUsageInBytes:                    megsToBytes(456),
						TimeSpentInCPU:                      112 * time.Second,
						MemoryLimitInBytes:                  megsToBytes(789),
						DiskLimitInBytes:                    megsToBytes(1024),
						ContainerAgeInNanoseconds:           0,
						AbsoluteCPUEntitlementInNanoseconds: 2004,
					},
				},
			}

			fakeExecutorClient.GetBulkMetricsReturnsOnCall(0, metricsAtT0, nil)
			fakeExecutorClient.GetBulkMetricsReturnsOnCall(1, metricsAtT10, nil)
			fakeExecutorClient.GetBulkMetricsReturnsOnCall(2, metricsAtT20, nil)
		})

		It("emits memory and disk usage for each container, but no CPU", func() {
			Eventually(sentMetrics).Should(ConsistOf([]logging.ContainerMetric{
				{
					CpuPercentage:          0.0,
					MemoryBytes:            metricsAtT0["container-guid-without-index"].ContainerMetrics.MemoryUsageInBytes,
					DiskBytes:              metricsAtT0["container-guid-without-index"].ContainerMetrics.DiskUsageInBytes,
					MemoryBytesQuota:       metricsAtT0["container-guid-without-index"].ContainerMetrics.MemoryLimitInBytes,
					DiskBytesQuota:         metricsAtT0["container-guid-without-index"].ContainerMetrics.DiskLimitInBytes,
					AbsoluteCPUUsage:       uint64(metricsAtT0["container-guid-without-index"].ContainerMetrics.TimeSpentInCPU),
					AbsoluteCPUEntitlement: metricsAtT0["container-guid-without-index"].ContainerMetrics.AbsoluteCPUEntitlementInNanoseconds,
					ContainerAge:           metricsAtT0["container-guid-without-index"].ContainerMetrics.ContainerAgeInNanoseconds,
					Tags: map[string]string{
						"source_id":   "source-id-without-index",
						"instance_id": "0",
					},
				},
				{
					CpuPercentage:          0.0,
					MemoryBytes:            metricsAtT0["container-guid-with-index"].ContainerMetrics.MemoryUsageInBytes,
					DiskBytes:              metricsAtT0["container-guid-with-index"].ContainerMetrics.DiskUsageInBytes,
					MemoryBytesQuota:       metricsAtT0["container-guid-with-index"].ContainerMetrics.MemoryLimitInBytes,
					DiskBytesQuota:         metricsAtT0["container-guid-with-index"].ContainerMetrics.DiskLimitInBytes,
					AbsoluteCPUUsage:       uint64(metricsAtT0["container-guid-with-index"].ContainerMetrics.TimeSpentInCPU),
					AbsoluteCPUEntitlement: metricsAtT0["container-guid-with-index"].ContainerMetrics.AbsoluteCPUEntitlementInNanoseconds,
					ContainerAge:           metricsAtT0["container-guid-with-index"].ContainerMetrics.ContainerAgeInNanoseconds,
					Tags: map[string]string{
						"source_id":   "source-id-with-index",
						"instance_id": "1",
					},
				},
				{
					CpuPercentage:          0.0,
					MemoryBytes:            metricsAtT0["container-guid-without-preloaded-rootfs"].ContainerMetrics.MemoryUsageInBytes,
					DiskBytes:              metricsAtT0["container-guid-without-preloaded-rootfs"].ContainerMetrics.DiskUsageInBytes,
					MemoryBytesQuota:       metricsAtT0["container-guid-without-preloaded-rootfs"].ContainerMetrics.MemoryLimitInBytes,
					DiskBytesQuota:         metricsAtT0["container-guid-without-preloaded-rootfs"].ContainerMetrics.DiskLimitInBytes,
					AbsoluteCPUUsage:       uint64(metricsAtT0["container-guid-without-preloaded-rootfs"].ContainerMetrics.TimeSpentInCPU),
					AbsoluteCPUEntitlement: metricsAtT0["container-guid-without-preloaded-rootfs"].ContainerMetrics.AbsoluteCPUEntitlementInNanoseconds,
					ContainerAge:           metricsAtT0["container-guid-without-preloaded-rootfs"].ContainerMetrics.ContainerAgeInNanoseconds,
					Tags: map[string]string{
						"source_id":   "source-id-without-preloaded-rootfs",
						"instance_id": "0",
					},
				},
				{
					CpuPercentage:          0.0,
					MemoryBytes:            metricsAtT0["container-guid-without-age"].ContainerMetrics.MemoryUsageInBytes,
					DiskBytes:              metricsAtT0["container-guid-without-age"].ContainerMetrics.DiskUsageInBytes,
					MemoryBytesQuota:       metricsAtT0["container-guid-without-age"].ContainerMetrics.MemoryLimitInBytes,
					DiskBytesQuota:         metricsAtT0["container-guid-without-age"].ContainerMetrics.DiskLimitInBytes,
					AbsoluteCPUUsage:       uint64(metricsAtT0["container-guid-without-age"].ContainerMetrics.TimeSpentInCPU),
					AbsoluteCPUEntitlement: metricsAtT0["container-guid-without-age"].ContainerMetrics.AbsoluteCPUEntitlementInNanoseconds,
					ContainerAge:           metricsAtT0["container-guid-without-age"].ContainerMetrics.ContainerAgeInNanoseconds,
					Tags: map[string]string{
						"source_id":   "source-id-without-age",
						"instance_id": "0",
					},
				},
			}))
		})

		It("emits container cpu usage for each container", func() {
			Eventually(sentCPUUsage).Should(ConsistOf(
				cpuUsage{applicationID: "source-id-without-index", absoluteUsage: uint64((100 * time.Second).Nanoseconds()), absoluteEntitlement: 2001, containerAge: 1001},
				cpuUsage{applicationID: "source-id-with-index", instanceIndex: 1, absoluteUsage: uint64((100 * time.Second).Nanoseconds()), absoluteEntitlement: 2002, containerAge: 1002},
				cpuUsage{applicationID: "source-id-without-preloaded-rootfs", absoluteUsage: uint64((100 * time.Second).Nanoseconds()), absoluteEntitlement: 2003, containerAge: 1003},
				cpuUsage{applicationID: "source-id-without-age", absoluteUsage: uint64((100 * time.Second).Nanoseconds()), absoluteEntitlement: 2004, containerAge: 0},
			))
		})

		Context("when contianers EnableContainerProxy is set", func() {
			BeforeEach(func() {
				containers := []executor.Container{
					{
						Guid:        "container-guid-without-index",
						MemoryLimit: megsToBytes(200 + proxyMemoryAllocationMB),
						RunInfo: executor.RunInfo{
							EnableContainerProxy: true,
							MetricsConfig:        executor.MetricsConfig{Tags: map[string]string{"source_id": "source-id-without-index"}},
						},
					},
					{
						Guid:        "container-guid-with-index",
						MemoryLimit: megsToBytes(400 + proxyMemoryAllocationMB),
						RunInfo: executor.RunInfo{
							EnableContainerProxy: true,
							MetricsConfig:        executor.MetricsConfig{Tags: map[string]string{"source_id": "source-id-with-index", "instance_id": "1"}},
						},
					},
					{
						Guid:        "container-guid-without-preloaded-rootfs",
						MemoryLimit: megsToBytes(200),
						RunInfo: executor.RunInfo{
							EnableContainerProxy: false,
							MetricsConfig:        executor.MetricsConfig{Tags: map[string]string{"source_id": "source-id-without-preloaded-rootfs"}},
						},
					},
				}
				fakeExecutorClient.ListContainersReturnsOnCall(0, containers, nil)
			})

			Context("when enableContainerProxy not set", func() {
				It("does not change the memory usage reported by garden", func() {
					appMemory := megsToBytes(123)
					expectedMemoryUsageWithoutIndex := float64(appMemory)
					Eventually(sentMetrics).Should(ContainElement(
						logging.ContainerMetric{
							CpuPercentage:          0.0,
							MemoryBytes:            uint64(expectedMemoryUsageWithoutIndex),
							DiskBytes:              metricsAtT0["container-guid-without-index"].ContainerMetrics.DiskUsageInBytes,
							MemoryBytesQuota:       metricsAtT0["container-guid-without-index"].ContainerMetrics.MemoryLimitInBytes,
							DiskBytesQuota:         metricsAtT0["container-guid-without-index"].ContainerMetrics.DiskLimitInBytes,
							AbsoluteCPUUsage:       uint64(metricsAtT0["container-guid-without-index"].ContainerMetrics.TimeSpentInCPU),
							AbsoluteCPUEntitlement: metricsAtT0["container-guid-without-index"].ContainerMetrics.AbsoluteCPUEntitlementInNanoseconds,
							ContainerAge:           metricsAtT0["container-guid-without-index"].ContainerMetrics.ContainerAgeInNanoseconds,
							Tags: map[string]string{
								"source_id":   "source-id-without-index",
								"instance_id": "0",
							},
						},
					))
				})
			})

			Context("when enableContainerProxy is set", func() {
				BeforeEach(func() {
					enableContainerProxy = true
				})

				It("emits a rescaled memory usage based on the additional memory allocation for the proxy", func() {
					appMemory := megsToBytes(123)
					expectedMemoryUsageWithoutIndex := float64(appMemory) * 200.0 / (200.0 + float64(proxyMemoryAllocationMB))
					expectedMemoryLimitWithoutIndex := float64(metricsAtT0["container-guid-without-index"].ContainerMetrics.MemoryLimitInBytes) - float64(megsToBytes(proxyMemoryAllocationMB))
					Eventually(sentMetrics).Should(ContainElement(
						logging.ContainerMetric{
							CpuPercentage:          0.0,
							MemoryBytes:            uint64(expectedMemoryUsageWithoutIndex),
							DiskBytes:              metricsAtT0["container-guid-without-index"].ContainerMetrics.DiskUsageInBytes,
							MemoryBytesQuota:       uint64(expectedMemoryLimitWithoutIndex),
							DiskBytesQuota:         metricsAtT0["container-guid-without-index"].ContainerMetrics.DiskLimitInBytes,
							AbsoluteCPUUsage:       uint64(metricsAtT0["container-guid-without-index"].ContainerMetrics.TimeSpentInCPU),
							AbsoluteCPUEntitlement: metricsAtT0["container-guid-without-index"].ContainerMetrics.AbsoluteCPUEntitlementInNanoseconds,
							ContainerAge:           metricsAtT0["container-guid-without-index"].ContainerMetrics.ContainerAgeInNanoseconds,
							Tags: map[string]string{
								"source_id":   "source-id-without-index",
								"instance_id": "0",
							},
						},
					))
				})

				Context("when there is a container without preloaded rootfs", func() {
					It("should emit memory usage that is not rescaled", func() {
						Eventually(sentMetrics).Should(ContainElement(
							logging.ContainerMetric{
								CpuPercentage:          0.0,
								MemoryBytes:            metricsAtT0["container-guid-without-preloaded-rootfs"].ContainerMetrics.MemoryUsageInBytes,
								DiskBytes:              metricsAtT0["container-guid-without-preloaded-rootfs"].ContainerMetrics.DiskUsageInBytes,
								MemoryBytesQuota:       metricsAtT0["container-guid-without-preloaded-rootfs"].ContainerMetrics.MemoryLimitInBytes,
								DiskBytesQuota:         metricsAtT0["container-guid-without-preloaded-rootfs"].ContainerMetrics.DiskLimitInBytes,
								AbsoluteCPUUsage:       uint64(metricsAtT0["container-guid-without-preloaded-rootfs"].ContainerMetrics.TimeSpentInCPU),
								AbsoluteCPUEntitlement: metricsAtT0["container-guid-without-preloaded-rootfs"].ContainerMetrics.AbsoluteCPUEntitlementInNanoseconds,
								ContainerAge:           metricsAtT0["container-guid-without-preloaded-rootfs"].ContainerMetrics.ContainerAgeInNanoseconds,
								Tags: map[string]string{
									"source_id":   "source-id-without-preloaded-rootfs",
									"instance_id": "0",
								},
							},
						))
					})
				})
			})
		})

		It("does not emit anything for containers with no metrics guid", func() {
			Consistently(sentMetrics).ShouldNot(ContainElement(WithTransform(func(m logging.ContainerMetric) string {
				return m.Tags["source_id"]
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
				fakeClock.WaitForWatcherAndIncrement(interval)
				Eventually(fakeExecutorClient.GetBulkMetricsCallCount).Should(Equal(3))

				Eventually(sentMetrics).Should(ContainElement(logging.ContainerMetric{
					CpuPercentage:          50.0,
					MemoryBytes:            metricsAtT10["container-guid-without-index"].ContainerMetrics.MemoryUsageInBytes,
					DiskBytes:              metricsAtT10["container-guid-without-index"].ContainerMetrics.DiskUsageInBytes,
					MemoryBytesQuota:       metricsAtT10["container-guid-without-index"].ContainerMetrics.MemoryLimitInBytes,
					DiskBytesQuota:         metricsAtT10["container-guid-without-index"].ContainerMetrics.DiskLimitInBytes,
					AbsoluteCPUUsage:       uint64(metricsAtT10["container-guid-without-index"].ContainerMetrics.TimeSpentInCPU),
					AbsoluteCPUEntitlement: metricsAtT10["container-guid-without-index"].ContainerMetrics.AbsoluteCPUEntitlementInNanoseconds,
					ContainerAge:           metricsAtT10["container-guid-without-index"].ContainerMetrics.ContainerAgeInNanoseconds,
					Tags: map[string]string{
						"source_id":   "source-id-without-index",
						"instance_id": "0",
					},
				}))

				Eventually(sentMetrics).Should(ContainElement(logging.ContainerMetric{
					CpuPercentage:          100.0,
					MemoryBytes:            metricsAtT10["container-guid-with-index"].ContainerMetrics.MemoryUsageInBytes,
					DiskBytes:              metricsAtT10["container-guid-with-index"].ContainerMetrics.DiskUsageInBytes,
					MemoryBytesQuota:       metricsAtT10["container-guid-with-index"].ContainerMetrics.MemoryLimitInBytes,
					DiskBytesQuota:         metricsAtT10["container-guid-with-index"].ContainerMetrics.DiskLimitInBytes,
					AbsoluteCPUUsage:       uint64(metricsAtT10["container-guid-with-index"].ContainerMetrics.TimeSpentInCPU),
					AbsoluteCPUEntitlement: metricsAtT10["container-guid-with-index"].ContainerMetrics.AbsoluteCPUEntitlementInNanoseconds,
					ContainerAge:           metricsAtT10["container-guid-with-index"].ContainerMetrics.ContainerAgeInNanoseconds,
					Tags: map[string]string{
						"source_id":   "source-id-with-index",
						"instance_id": "1",
					},
				}))

				Eventually(sentMetrics).Should(ContainElement(logging.ContainerMetric{
					CpuPercentage:          100.0,
					MemoryBytes:            metricsAtT10["container-guid-without-preloaded-rootfs"].ContainerMetrics.MemoryUsageInBytes,
					DiskBytes:              metricsAtT10["container-guid-without-preloaded-rootfs"].ContainerMetrics.DiskUsageInBytes,
					MemoryBytesQuota:       metricsAtT10["container-guid-without-preloaded-rootfs"].ContainerMetrics.MemoryLimitInBytes,
					DiskBytesQuota:         metricsAtT10["container-guid-without-preloaded-rootfs"].ContainerMetrics.DiskLimitInBytes,
					AbsoluteCPUUsage:       uint64(metricsAtT10["container-guid-without-preloaded-rootfs"].ContainerMetrics.TimeSpentInCPU),
					AbsoluteCPUEntitlement: metricsAtT10["container-guid-without-preloaded-rootfs"].ContainerMetrics.AbsoluteCPUEntitlementInNanoseconds,
					ContainerAge:           metricsAtT10["container-guid-without-preloaded-rootfs"].ContainerMetrics.ContainerAgeInNanoseconds,
					Tags: map[string]string{
						"source_id":   "source-id-without-preloaded-rootfs",
						"instance_id": "0",
					},
				}))
			})

			Context("Metrics", func() {
				It("returns the cached metrics last emitted", func() {
					containerMetrics := reporter.Metrics()
					Expect(containerMetrics).To(HaveLen(5))
					Expect(containerMetrics).To(HaveKeyWithValue("container-guid-without-index", &containermetrics.CachedContainerMetrics{
						MetricGUID:       "source-id-without-index",
						CPUUsageFraction: 0.5,
						MemoryUsageBytes: megsToBytes(1230),
						DiskUsageBytes:   4560,
						MemoryQuotaBytes: megsToBytes(7890),
						DiskQuotaBytes:   4096,
					}))
					Expect(containerMetrics).To(HaveKeyWithValue("container-guid-with-index", &containermetrics.CachedContainerMetrics{
						MetricGUID:       "source-id-with-index",
						CPUUsageFraction: 1.0,
						MemoryUsageBytes: megsToBytes(3210),
						DiskUsageBytes:   6540,
						MemoryQuotaBytes: megsToBytes(9870),
						DiskQuotaBytes:   512,
					}))
					Expect(containerMetrics).To(HaveKeyWithValue("container-guid-without-source-id", &containermetrics.CachedContainerMetrics{
						MetricGUID:       "",
						CPUUsageFraction: 0.1,
						MemoryUsageBytes: megsToBytes(123),
						DiskUsageBytes:   megsToBytes(456),
						MemoryQuotaBytes: megsToBytes(789),
						DiskQuotaBytes:   megsToBytes(1024),
					}))
					Expect(containerMetrics).To(HaveKeyWithValue("container-guid-without-preloaded-rootfs", &containermetrics.CachedContainerMetrics{
						MetricGUID:       "source-id-without-preloaded-rootfs",
						CPUUsageFraction: 1.0,
						MemoryUsageBytes: megsToBytes(3450),
						DiskUsageBytes:   4560,
						MemoryQuotaBytes: megsToBytes(6780),
						DiskQuotaBytes:   2048,
					}))
					Expect(containerMetrics).To(HaveKeyWithValue("container-guid-without-age", &containermetrics.CachedContainerMetrics{
						MetricGUID:       "source-id-without-age",
						CPUUsageFraction: 0.5,
						MemoryUsageBytes: megsToBytes(123),
						DiskUsageBytes:   megsToBytes(456),
						MemoryQuotaBytes: megsToBytes(789),
						DiskQuotaBytes:   megsToBytes(1024),
					}))
				})
			})

			Context("and the interval elapses again", func() {
				JustBeforeEach(func() {
					fakeClock.WaitForWatcherAndIncrement(interval)
					Eventually(fakeExecutorClient.GetBulkMetricsCallCount).Should(Equal(3))
				})

				It("emits the new memory and disk usage, and the computed CPU percent", func() {
					Eventually(sentMetrics).Should(ContainElement(logging.ContainerMetric{
						CpuPercentage:          20.0,
						MemoryBytes:            metricsAtT20["container-guid-without-index"].ContainerMetrics.MemoryUsageInBytes,
						DiskBytes:              metricsAtT20["container-guid-without-index"].ContainerMetrics.DiskUsageInBytes,
						MemoryBytesQuota:       metricsAtT20["container-guid-without-index"].ContainerMetrics.MemoryLimitInBytes,
						DiskBytesQuota:         metricsAtT20["container-guid-without-index"].ContainerMetrics.DiskLimitInBytes,
						AbsoluteCPUUsage:       uint64(metricsAtT20["container-guid-without-index"].ContainerMetrics.TimeSpentInCPU),
						AbsoluteCPUEntitlement: metricsAtT20["container-guid-without-index"].ContainerMetrics.AbsoluteCPUEntitlementInNanoseconds,
						ContainerAge:           metricsAtT20["container-guid-without-index"].ContainerMetrics.ContainerAgeInNanoseconds,
						Tags: map[string]string{
							"source_id":   "source-id-without-index",
							"instance_id": "0",
						},
					}))

					Eventually(sentMetrics).Should(ContainElement(logging.ContainerMetric{
						CpuPercentage:          20.0,
						MemoryBytes:            metricsAtT20["container-guid-with-index"].ContainerMetrics.MemoryUsageInBytes,
						DiskBytes:              metricsAtT20["container-guid-with-index"].ContainerMetrics.DiskUsageInBytes,
						MemoryBytesQuota:       metricsAtT20["container-guid-with-index"].ContainerMetrics.MemoryLimitInBytes,
						DiskBytesQuota:         metricsAtT20["container-guid-with-index"].ContainerMetrics.DiskLimitInBytes,
						AbsoluteCPUUsage:       uint64(metricsAtT20["container-guid-with-index"].ContainerMetrics.TimeSpentInCPU),
						AbsoluteCPUEntitlement: metricsAtT20["container-guid-with-index"].ContainerMetrics.AbsoluteCPUEntitlementInNanoseconds,
						ContainerAge:           metricsAtT20["container-guid-with-index"].ContainerMetrics.ContainerAgeInNanoseconds,
						Tags: map[string]string{
							"source_id":   "source-id-with-index",
							"instance_id": "1",
						},
					}))

					Eventually(sentMetrics).Should(ContainElement(logging.ContainerMetric{
						CpuPercentage:          20.0,
						MemoryBytes:            metricsAtT20["container-guid-without-preloaded-rootfs"].ContainerMetrics.MemoryUsageInBytes,
						DiskBytes:              metricsAtT20["container-guid-without-preloaded-rootfs"].ContainerMetrics.DiskUsageInBytes,
						MemoryBytesQuota:       metricsAtT20["container-guid-without-preloaded-rootfs"].ContainerMetrics.MemoryLimitInBytes,
						DiskBytesQuota:         metricsAtT20["container-guid-without-preloaded-rootfs"].ContainerMetrics.DiskLimitInBytes,
						AbsoluteCPUUsage:       uint64(metricsAtT20["container-guid-without-preloaded-rootfs"].ContainerMetrics.TimeSpentInCPU),
						AbsoluteCPUEntitlement: metricsAtT20["container-guid-without-preloaded-rootfs"].ContainerMetrics.AbsoluteCPUEntitlementInNanoseconds,
						ContainerAge:           metricsAtT20["container-guid-without-preloaded-rootfs"].ContainerMetrics.ContainerAgeInNanoseconds,
						Tags: map[string]string{
							"source_id":   "source-id-without-preloaded-rootfs",
							"instance_id": "0",
						},
					}))
				})
			})
		})
	})

	Context("when the metric tags are provided", func() {
		BeforeEach(func() {
			containers := []executor.Container{
				{Guid: "container-0"},
			}
			fakeExecutorClient.ListContainersReturnsOnCall(0, containers, nil)
		})

		Context("when the source_id tag is set in metrics config for a container", func() {
			BeforeEach(func() {
				metricsMap := map[string]executor.Metrics{
					"container-0": {
						executor.MetricsConfig{Tags: map[string]string{"source_id": "some-source-id"}, Guid: "some-metric-guid"},
						executor.ContainerMetrics{},
					},
				}
				fakeExecutorClient.GetBulkMetricsReturns(metricsMap, nil)
			})

			It("sends a container metric with the correct application id value", func() {
				fakeClock.WaitForWatcherAndIncrement(interval)
				Eventually(fakeExecutorClient.GetBulkMetricsCallCount).Should(Equal(2))
				Eventually(fakeMetronClient.SendAppMetricsCallCount).Should(Equal(1))
				Expect(fakeMetronClient.SendAppMetricsArgsForCall(0).Tags).To(HaveKeyWithValue("source_id", "some-source-id"))
			})
		})

		Context("when there is no source_id tag set in metrics config for a container and the metricGuid is not set", func() {
			BeforeEach(func() {
				metricsMap := map[string]executor.Metrics{
					"container-0": {
						executor.MetricsConfig{Tags: map[string]string{"some-key": "some-value"}},
						executor.ContainerMetrics{},
					},
				}
				fakeExecutorClient.GetBulkMetricsReturns(metricsMap, nil)
			})

			It("will not emit any metrics", func() {
				fakeClock.WaitForWatcherAndIncrement(interval)
				Eventually(fakeExecutorClient.GetBulkMetricsCallCount).Should(Equal(2))
				Consistently(fakeMetronClient.SendAppMetricsCallCount).Should(Equal(0))
			})
		})

		Context("when instance id tag set in metrics config for container", func() {
			BeforeEach(func() {
				metricsMap := map[string]executor.Metrics{
					"container-0": {
						executor.MetricsConfig{Tags: map[string]string{"source_id": "some-source-id", "instance_id": "99"}, Index: 1},
						executor.ContainerMetrics{},
					},
				}
				fakeExecutorClient.GetBulkMetricsReturns(metricsMap, nil)
			})

			It("sends a container metric with the correct instance id value", func() {
				fakeClock.WaitForWatcherAndIncrement(interval)
				Eventually(fakeExecutorClient.GetBulkMetricsCallCount).Should(Equal(2))
				Eventually(fakeMetronClient.SendAppMetricsCallCount).Should(Equal(1))
				Expect(fakeMetronClient.SendAppMetricsArgsForCall(0).Tags).To(HaveKeyWithValue("instance_id", "99"))
			})
		})

		Context("when instance id tag not set in metrics config for container", func() {
			BeforeEach(func() {
				metricsMap := map[string]executor.Metrics{
					"container-0": {
						executor.MetricsConfig{Tags: map[string]string{"source_id": "some-source-id"}, Index: 1},
						executor.ContainerMetrics{},
					},
				}
				fakeExecutorClient.GetBulkMetricsReturns(metricsMap, nil)
			})

			It("sends a container metric with the correct instance id value", func() {
				fakeClock.WaitForWatcherAndIncrement(interval)
				Eventually(fakeExecutorClient.GetBulkMetricsCallCount).Should(Equal(2))
				Eventually(fakeMetronClient.SendAppMetricsCallCount).Should(Equal(1))
				Expect(fakeMetronClient.SendAppMetricsArgsForCall(0).Tags).To(HaveKeyWithValue("instance_id", "1"))
			})
		})

		Context("when instance id tag set to non-integer value in metrics config for container", func() {
			BeforeEach(func() {
				metricsMap := map[string]executor.Metrics{
					"container-0": {
						executor.MetricsConfig{Tags: map[string]string{"source_id": "some-source-id", "instance_id": "some-instance-id"}, Index: 1},
						executor.ContainerMetrics{},
					},
				}
				fakeExecutorClient.GetBulkMetricsReturns(metricsMap, nil)
			})

			It("keeps the metrics tag value of instance id", func() {
				fakeClock.WaitForWatcherAndIncrement(interval)
				Eventually(fakeExecutorClient.GetBulkMetricsCallCount).Should(Equal(2))
				Eventually(fakeMetronClient.SendAppMetricsCallCount).Should(Equal(1))
				Expect(fakeMetronClient.SendAppMetricsArgsForCall(0).Tags).To(HaveKeyWithValue("instance_id", "some-instance-id"))
			})
		})
	})

	Context("when metric tags are not provided", func() {
		BeforeEach(func() {
			containers := []executor.Container{
				{Guid: "container-0"},
			}
			fakeExecutorClient.ListContainersReturnsOnCall(0, containers, nil)
		})

		Context("when the metrics guid is provided", func() {
			BeforeEach(func() {
				metricsMap := map[string]executor.Metrics{
					"container-0": {
						executor.MetricsConfig{Guid: "some-metric-guid"},
						executor.ContainerMetrics{},
					},
				}
				fakeExecutorClient.GetBulkMetricsReturns(metricsMap, nil)
			})

			It("sends a container metric with the correct application id value", func() {
				fakeClock.WaitForWatcherAndIncrement(interval)
				Eventually(fakeExecutorClient.GetBulkMetricsCallCount).Should(Equal(2))
				Eventually(fakeMetronClient.SendAppMetricsCallCount).Should(Equal(1))
				Expect(fakeMetronClient.SendAppMetricsArgsForCall(0).Tags).To(HaveKeyWithValue("source_id", "some-metric-guid"))
			})
		})

		Context("when the metrics guid is not provided", func() {
			BeforeEach(func() {
				metricsMap := map[string]executor.Metrics{
					"container-0": {
						executor.MetricsConfig{Guid: ""},
						executor.ContainerMetrics{},
					},
				}
				fakeExecutorClient.GetBulkMetricsReturns(metricsMap, nil)
			})

			It("will not emit any metrics", func() {
				fakeClock.WaitForWatcherAndIncrement(interval)
				Eventually(fakeExecutorClient.GetBulkMetricsCallCount).Should(Equal(2))
				Consistently(fakeMetronClient.SendAppMetricsCallCount).Should(Equal(0))
			})
		})
	})
})
