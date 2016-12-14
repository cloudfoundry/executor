package initializer_test

import (
	"net/http"
	"time"

	"code.cloudfoundry.org/clock/fakeclock"
	"code.cloudfoundry.org/executor/initializer"
	"code.cloudfoundry.org/executor/initializer/configuration"
	"code.cloudfoundry.org/garden"
	"code.cloudfoundry.org/lager/lagertest"
	fake_metric "github.com/cloudfoundry/dropsonde/metric_sender/fake"
	"github.com/cloudfoundry/dropsonde/metrics"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
)

var _ = Describe("Initializer", func() {
	var initialTime time.Time
	var sender *fake_metric.FakeMetricSender
	var fakeGarden *ghttp.Server
	var fakeClock *fakeclock.FakeClock
	var errCh chan error
	var done chan struct{}
	var config initializer.ExecutorConfig

	BeforeEach(func() {
		initialTime = time.Now()
		sender = fake_metric.NewFakeMetricSender()
		metrics.Initialize(sender, nil)
		fakeGarden = ghttp.NewUnstartedServer()
		fakeClock = fakeclock.NewFakeClock(initialTime)
		errCh = make(chan error, 1)
		done = make(chan struct{})

		fakeGarden.RouteToHandler("GET", "/ping", ghttp.RespondWithJSONEncoded(http.StatusOK, struct{}{}))
		fakeGarden.RouteToHandler("GET", "/containers", ghttp.RespondWithJSONEncoded(http.StatusOK, struct{}{}))
		fakeGarden.RouteToHandler("GET", "/capacity", ghttp.RespondWithJSONEncoded(http.StatusOK,
			garden.Capacity{MemoryInBytes: 1024 * 1024 * 1024, DiskInBytes: 2048 * 1024 * 1024, MaxContainers: 4}))
		fakeGarden.RouteToHandler("GET", "/containers/bulk_info", ghttp.RespondWithJSONEncoded(http.StatusOK, struct{}{}))
		config = initializer.ExecutorConfig{
			CachePath:                          "/tmp/cache",
			ContainerInodeLimit:                200000,
			ContainerMaxCpuShares:              0,
			ContainerMetricsReportInterval:     initializer.Duration(15 * time.Second),
			ContainerOwnerName:                 "executor",
			ContainerReapInterval:              initializer.Duration(time.Minute),
			CreateWorkPoolSize:                 32,
			DeleteWorkPoolSize:                 32,
			DiskMB:                             configuration.Automatic,
			ExportNetworkEnvVars:               false,
			GardenAddr:                         "/tmp/garden.sock",
			GardenHealthcheckCommandRetryPause: initializer.Duration(1 * time.Second),
			GardenHealthcheckEmissionInterval:  initializer.Duration(30 * time.Second),
			GardenHealthcheckInterval:          initializer.Duration(10 * time.Minute),
			GardenHealthcheckProcessArgs:       []string{},
			GardenHealthcheckProcessEnv:        []string{},
			GardenHealthcheckTimeout:           initializer.Duration(10 * time.Minute),
			GardenNetwork:                      "unix",
			HealthCheckContainerOwnerName:      "executor-health-check",
			HealthCheckWorkPoolSize:            64,
			HealthyMonitoringInterval:          initializer.Duration(30 * time.Second),
			MaxCacheSizeInBytes:                10 * 1024 * 1024 * 1024,
			MaxConcurrentDownloads:             5,
			MemoryMB:                           configuration.Automatic,
			MetricsWorkPoolSize:                8,
			ReadWorkPoolSize:                   64,
			ReservedExpirationTime:             initializer.Duration(time.Minute),
			SkipCertVerify:                     false,
			TempDir:                            "/tmp",
			UnhealthyMonitoringInterval:        initializer.Duration(500 * time.Millisecond),
			VolmanDriverPaths:                  "/tmpvolman1:/tmp/volman2",
		}
	})

	AfterEach(func() {
		Eventually(done).Should(BeClosed())
		fakeGarden.Close()
	})

	JustBeforeEach(func() {
		fakeGarden.Start()
		config.GardenAddr = fakeGarden.HTTPTestServer.Listener.Addr().String()
		config.GardenNetwork = "tcp"
		go func() {
			_, _, err := initializer.Initialize(lagertest.NewTestLogger("test"), config, "fake-rootfs", fakeClock)
			errCh <- err
			close(done)
		}()
	})

	checkStalledMetric := func() float64 {
		return sender.GetValue("StalledGardenDuration").Value
	}

	Context("when garden doesn't respond", func() {
		var waitChan chan struct{}

		BeforeEach(func() {
			waitChan = make(chan struct{})
			fakeGarden.RouteToHandler("GET", "/ping", func(w http.ResponseWriter, req *http.Request) {
				<-waitChan
				ghttp.RespondWithJSONEncoded(http.StatusOK, struct{}{})(w, req)
			})
		})

		AfterEach(func() {
			close(waitChan)
		})

		It("emits metrics when garden doesn't respond", func() {
			Consistently(checkStalledMetric, 10*time.Millisecond).Should(BeEquivalentTo(0))
			fakeClock.WaitForWatcherAndIncrement(initializer.StalledMetricHeartbeatInterval)
			Eventually(checkStalledMetric).Should(BeNumerically("~", fakeClock.Since(initialTime)))
		})
	})

	Context("when garden responds", func() {
		It("emits 0", func() {
			Eventually(func() bool { return sender.HasValue("StalledGardenDuration") }).Should(BeTrue())
			Expect(checkStalledMetric()).To(BeEquivalentTo(0))
			Consistently(errCh).ShouldNot(Receive(HaveOccurred()))
		})
	})

	Context("when garden responds with an error", func() {
		var retried chan struct{}

		BeforeEach(func() {
			callCount := 0
			retried = make(chan struct{})
			fakeGarden.RouteToHandler("GET", "/ping", func(w http.ResponseWriter, req *http.Request) {
				callCount++
				if callCount == 1 {
					ghttp.RespondWith(http.StatusInternalServerError, "")(w, req)
				} else if callCount == 2 {
					ghttp.RespondWithJSONEncoded(http.StatusOK, struct{}{})(w, req)
					close(retried)
				}
			})
		})

		It("retries on a timer until it succeeds", func() {
			Consistently(retried).ShouldNot(BeClosed())
			fakeClock.Increment(initializer.PingGardenInterval)
			Eventually(retried).Should(BeClosed())
		})

		It("emits zero once it succeeds", func() {
			Consistently(func() bool { return sender.HasValue("StalledGardenDuration") }).Should(BeFalse())
			fakeClock.Increment(initializer.PingGardenInterval)
			Eventually(func() bool { return sender.HasValue("StalledGardenDuration") }).Should(BeTrue())
			Expect(checkStalledMetric()).To(BeEquivalentTo(0))
		})

		Context("when the error is unrecoverable", func() {
			BeforeEach(func() {
				fakeGarden.RouteToHandler(
					"GET",
					"/ping",
					ghttp.RespondWith(http.StatusGatewayTimeout, `{ "Type": "UnrecoverableError" , "Message": "Extra Special Error Message"}`),
				)
			})

			It("returns an error", func() {
				Eventually(errCh).Should(Receive(BeAssignableToTypeOf(garden.UnrecoverableError{})))
			})
		})
	})

	Context("when the post setup hook is invalid", func() {
		BeforeEach(func() {
			config.PostSetupHook = "unescaped quote\\"
		})

		It("fails fast", func() {
			Eventually(errCh).Should(Receive(MatchError("EOF found after escape character")))
		})
	})

	Describe("configuring trusted CA bundle", func() {
		Context("when valid", func() {
			BeforeEach(func() {
				config.PathToCACertsForDownloads = "fixtures/ca-certs"
			})

			It("uses it for the cached downloader", func() {
				Consistently(errCh).ShouldNot(Receive(HaveOccurred()))
			})

			Context("when the cert bundle has extra leading and trailing spaces", func() {
				BeforeEach(func() {
					config.PathToCACertsForDownloads = "fixtures/ca-certs-with-spaces"
				})

				It("does not error", func() {
					Consistently(errCh).ShouldNot(Receive(HaveOccurred()))
				})
			})

			Context("when the cert bundle is empty", func() {
				BeforeEach(func() {
					config.PathToCACertsForDownloads = "fixtures/ca-certs-empty"
				})

				It("does not error", func() {
					Consistently(errCh).ShouldNot(Receive(HaveOccurred()))
				})
			})
		})

		Context("when certs are invalid", func() {
			BeforeEach(func() {
				config.PathToCACertsForDownloads = "fixtures/ca-certs-invalid"
			})

			It("fails", func() {
				Eventually(errCh).Should(Receive(MatchError("unable to load CA certificate")))
			})
		})

		Context("when path is invalid", func() {
			BeforeEach(func() {
				config.PathToCACertsForDownloads = "sandwich"
			})

			It("fails", func() {
				Eventually(errCh).Should(Receive(MatchError("Unable to open CA cert bundle 'sandwich'")))
			})
		})
	})
})
