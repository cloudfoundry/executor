package initializer_test

import (
	"net/http"
	"time"

	"github.com/cloudfoundry-incubator/executor/initializer"
	"github.com/cloudfoundry-incubator/garden"
	fake_metric "github.com/cloudfoundry/dropsonde/metric_sender/fake"
	"github.com/cloudfoundry/dropsonde/metrics"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
	"github.com/pivotal-golang/clock/fakeclock"
	"github.com/pivotal-golang/lager/lagertest"
)

var _ = Describe("Initializer", func() {
	var initialTime time.Time
	var sender *fake_metric.FakeMetricSender
	var fakeGarden *ghttp.Server
	var fakeClock *fakeclock.FakeClock
	var errCh chan error
	var done chan struct{}
	var config initializer.Configuration

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
		config = initializer.DefaultConfiguration
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
			_, _, err := initializer.Initialize(lagertest.NewTestLogger("test"), config, fakeClock)
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
			fakeClock.Increment(initializer.StalledMetricHeartbeatInterval)
			Eventually(checkStalledMetric).Should(BeNumerically("~", fakeClock.Since(initialTime)))
		})
	})

	Context("when garden responds", func() {
		It("emits 0", func() {
			Eventually(func() bool { return sender.HasValue("StalledGardenDuration") }).Should(BeTrue())
			Expect(checkStalledMetric()).To(BeEquivalentTo(0))
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
				Eventually(errCh).Should(Receive(HaveOccurred()))
			})
		})
	})

	Context("when the post setup hook is invalid", func() {
		BeforeEach(func() {
			config.PostSetupHook = "unescaped quote\\"
		})

		It("fails fast", func() {
			Eventually(errCh).Should(Receive(HaveOccurred()))
		})
	})
})
