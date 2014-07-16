package executor_test

import (
	"os"
	"sync"
	"syscall"
	"time"

	. "github.com/cloudfoundry-incubator/executor/executor"
	"github.com/cloudfoundry-incubator/executor/registry"
	"github.com/cloudfoundry-incubator/executor/transformer"
	"github.com/cloudfoundry-incubator/executor/uploader/fake_uploader"
	"github.com/cloudfoundry-incubator/garden/client/fake_warden_client"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/cloudfoundry/gunk/timeprovider"
	"github.com/cloudfoundry/loggregatorlib/emitter"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/pivotal-golang/archiver/compressor/fake_compressor"
	"github.com/pivotal-golang/archiver/extractor/fake_extractor"
	"github.com/pivotal-golang/cacheddownloader/fakecacheddownloader"
)

var _ = Describe("Executor", func() {
	var (
		executor     *Executor
		wardenClient *fake_warden_client.FakeClient
		logger       *steno.Logger
		trans        *transformer.Transformer
		reg          registry.Registry
	)

	BeforeEach(func() {
		logEmitter, err := emitter.NewEmitter("", "", "", "", nil)
		Ω(err).ShouldNot(HaveOccurred())

		wardenClient = fake_warden_client.New()
		trans = transformer.NewTransformer(
			logEmitter,
			fakecacheddownloader.New(),
			new(fake_uploader.FakeUploader),
			&fake_extractor.FakeExtractor{},
			&fake_compressor.FakeCompressor{},
			logger,
			"/tmp",
		)
		capacity := registry.Capacity{MemoryMB: 1024, DiskMB: 1024, Containers: 42}
		reg = registry.New(capacity, timeprovider.NewTimeProvider())
		runWaitGroup := new(sync.WaitGroup)
		runCanceller := make(chan struct{})
		steno.EnterTestMode()
		logger = steno.NewLogger("test-logger")

		executor = New("executor", wardenClient, time.Second, runWaitGroup, runCanceller, logger)
	})

	Describe("Run", func() {
		var errChan chan error
		var sigChan chan os.Signal

		BeforeEach(func() {
			errChan = make(chan error)
			sigChan = make(chan os.Signal)
			ready := make(chan struct{})
			go func() {
				errChan <- executor.Run(sigChan, ready)
			}()
			Eventually(ready).Should(BeClosed())
		})

		Context("after receiving SIGINT", func() {
			var err error
			BeforeEach(func() {
				sigChan <- syscall.SIGTERM
				err = <-errChan
			})

			It("completes without error", func() {
				Ω(err).Should(BeNil())
			})
		})
	})
})
