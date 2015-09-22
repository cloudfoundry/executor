package initializer_test

import (
	"net/http"
	"time"

	"github.com/cloudfoundry-incubator/executor/initializer"
	"github.com/cloudfoundry-incubator/garden"
	fake_metric "github.com/cloudfoundry/dropsonde/metric_sender/fake"
	"github.com/cloudfoundry/dropsonde/metrics"
	"github.com/pivotal-golang/lager/lagertest"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
)

var _ = Describe("Initializer", func() {
	var sender *fake_metric.FakeMetricSender
	var fakeGarden *ghttp.Server

	BeforeEach(func() {
		sender = fake_metric.NewFakeMetricSender()
		metrics.Initialize(sender, nil)
		fakeGarden = ghttp.NewUnstartedServer()
	})

	AfterEach(func() {
		fakeGarden.Close()
	})

	JustBeforeEach(func() {
		fakeGarden.Start()
		config := initializer.DefaultConfiguration
		config.GardenAddr = fakeGarden.HTTPTestServer.Listener.Addr().String()
		config.GardenNetwork = "tcp"
		go initializer.Initialize(lagertest.NewTestLogger("test"), config)
	})

	Context("when garden doesn't respond", func() {
		var waitChan chan struct{}

		BeforeEach(func() {
			waitChan = make(chan struct{})
			fakeGarden.RouteToHandler("GET", "/ping", func(http.ResponseWriter, *http.Request) {
				<-waitChan
			})
		})

		AfterEach(func() {
			close(waitChan)
		})

		It("emits metrics when garden doesn't respond", func() {
			Eventually(func() float64 {
				return sender.GetValue("rep.stalled_duration").Value
			}, 2*time.Second).Should(BeNumerically(">", 0))
		})

	})

	Context("when garden responds", func() {
		BeforeEach(func() {
			fakeGarden.RouteToHandler("GET", "/ping", ghttp.RespondWithJSONEncoded(http.StatusOK, struct{}{}))
			fakeGarden.RouteToHandler("GET", "/containers", ghttp.RespondWithJSONEncoded(http.StatusOK, struct{}{}))
			fakeGarden.RouteToHandler("GET", "/capacity", ghttp.RespondWithJSONEncoded(http.StatusOK,
				garden.Capacity{MemoryInBytes: 1024 * 1024 * 1024, DiskInBytes: 2048 * 1024 * 1024, MaxContainers: 4}))
			fakeGarden.RouteToHandler("GET", "/containers/bulk_info", ghttp.RespondWithJSONEncoded(http.StatusOK, struct{}{}))
		})

		It("emits 0", func() {
			Consistently(func() float64 {
				return sender.GetValue("rep.stalled_duration").Value
			}).Should(BeEquivalentTo(0))
		})
	})

	Context("when garden responds with an error", func() {
		var retried bool

		BeforeEach(func() {
			callCount := 0
			fakeGarden.RouteToHandler("GET", "/containers", ghttp.RespondWithJSONEncoded(http.StatusOK, struct{}{}))
			fakeGarden.RouteToHandler("GET", "/capacity", ghttp.RespondWithJSONEncoded(http.StatusOK,
				garden.Capacity{MemoryInBytes: 1024 * 1024 * 1024, DiskInBytes: 2048 * 1024 * 1024, MaxContainers: 4}))
			fakeGarden.RouteToHandler("GET", "/containers/bulk_info", ghttp.RespondWithJSONEncoded(http.StatusOK, struct{}{}))
			fakeGarden.RouteToHandler("GET", "/ping", func(w http.ResponseWriter, req *http.Request) {
				callCount++
				if callCount == 1 {
					ghttp.RespondWith(http.StatusInternalServerError, "")(w, req)
				} else if callCount == 2 {
					ghttp.RespondWithJSONEncoded(http.StatusOK, struct{}{})(w, req)
					retried = true
				}
			})
		})

		It("retries and emits zero on success", func() {
			Eventually(func() float64 {
				return sender.GetValue("rep.stalled_duration").Value
			}).Should(BeNumerically(">", 0))
			Eventually(func() bool {
				return retried
			}, 4*time.Second).Should(BeTrue())
			Eventually(func() float64 {
				return sender.GetValue("rep.stalled_duration").Value
			}).Should(BeEquivalentTo(0))
		})

	})
})
