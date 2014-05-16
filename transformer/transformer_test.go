package transformer_test

import (
	"github.com/cloudfoundry-incubator/garden/client/fake_warden_client"
	"github.com/cloudfoundry-incubator/garden/warden"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/pivotal-golang/archiver/compressor"
	"github.com/pivotal-golang/archiver/compressor/fake_compressor"
	"github.com/pivotal-golang/archiver/extractor"
	"github.com/pivotal-golang/archiver/extractor/fake_extractor"
	"github.com/pivotal-golang/cacheddownloader"
	"github.com/pivotal-golang/cacheddownloader/fakecacheddownloader"

	"github.com/cloudfoundry-incubator/executor/log_streamer"
	"github.com/cloudfoundry-incubator/executor/log_streamer/fake_log_streamer"
	"github.com/cloudfoundry-incubator/executor/sequence"
	"github.com/cloudfoundry-incubator/executor/steps/download_step"
	"github.com/cloudfoundry-incubator/executor/steps/emit_progress_step"
	"github.com/cloudfoundry-incubator/executor/steps/fetch_result_step"
	"github.com/cloudfoundry-incubator/executor/steps/parallel_step"
	"github.com/cloudfoundry-incubator/executor/steps/run_step"
	"github.com/cloudfoundry-incubator/executor/steps/try_step"
	"github.com/cloudfoundry-incubator/executor/steps/upload_step"
	. "github.com/cloudfoundry-incubator/executor/transformer"
	"github.com/cloudfoundry-incubator/executor/uploader"
	"github.com/cloudfoundry-incubator/executor/uploader/fake_uploader"
)

var _ = Describe("Transformer", func() {
	var (
		cachedDownloader cacheddownloader.CachedDownloader
		logger           *steno.Logger
		logStreamer      *fake_log_streamer.FakeLogStreamer
		uploader         uploader.Uploader
		extractor        extractor.Extractor
		compressor       compressor.Compressor
		wardenClient     *fake_warden_client.FakeClient
		transformer      *Transformer
		result           string
	)

	handle := "some-container-handle"

	BeforeEach(func() {
		logStreamer = fake_log_streamer.New()
		cachedDownloader = fakecacheddownloader.New()
		uploader = &fake_uploader.FakeUploader{}
		extractor = &fake_extractor.FakeExtractor{}
		compressor = &fake_compressor.FakeCompressor{}
		wardenClient = fake_warden_client.New()
		logger = &steno.Logger{}

		logStreamerFactory := func(models.LogConfig) log_streamer.LogStreamer {
			return logStreamer
		}

		transformer = NewTransformer(
			logStreamerFactory,
			cachedDownloader,
			uploader,
			extractor,
			compressor,
			logger,
			"/fake/temp/dir",
		)
	})

	It("is correct", func() {
		runActionModel := models.RunAction{Script: "do-something"}
		downloadActionModel := models.DownloadAction{From: "/file/to/download"}
		uploadActionModel := models.UploadAction{From: "/file/to/upload"}
		fetchResultActionModel := models.FetchResultAction{File: "some-file"}
		tryActionModel := models.TryAction{Action: models.ExecutorAction{runActionModel}}

		parallelActionModel := models.ParallelAction{
			Actions: []models.ExecutorAction{
				{downloadActionModel},
				{runActionModel},
			},
		}

		emitProgressActionModel := models.EmitProgressAction{
			Action:         models.ExecutorAction{runActionModel},
			StartMessage:   "starting",
			SuccessMessage: "successing",
			FailureMessage: "failuring",
		}

		actions := []models.ExecutorAction{
			{runActionModel},
			{downloadActionModel},
			{uploadActionModel},
			{fetchResultActionModel},
			{tryActionModel},
			{parallelActionModel},
			{emitProgressActionModel},
		}

		container, err := wardenClient.Create(warden.ContainerSpec{Handle: handle})
		Ω(err).ShouldNot(HaveOccurred())

		Ω(transformer.StepsFor(models.LogConfig{}, actions, container, &result)).To(Equal([]sequence.Step{
			run_step.New(
				container,
				runActionModel,
				logStreamer,
				logger,
			),
			download_step.New(
				container,
				downloadActionModel,
				cachedDownloader,
				extractor,
				"/fake/temp/dir",
				logger,
			),
			upload_step.New(
				container,
				uploadActionModel,
				uploader,
				compressor,
				"/fake/temp/dir",
				logStreamer,
				logger,
			),
			fetch_result_step.New(
				container,
				fetchResultActionModel,
				"/fake/temp/dir",
				logger,
				&result,
			),
			try_step.New(
				run_step.New(
					container,
					runActionModel,
					logStreamer,
					logger,
				),
				logger,
			),
			parallel_step.New(
				[]sequence.Step{
					download_step.New(
						container,
						downloadActionModel,
						cachedDownloader,
						extractor,
						"/fake/temp/dir",
						logger,
					),
					run_step.New(
						container,
						runActionModel,
						logStreamer,
						logger,
					),
				},
			),
			emit_progress_step.New(
				run_step.New(
					container,
					runActionModel,
					logStreamer,
					logger,
				),
				"starting",
				"successing",
				"failuring",
				logStreamer,
				logger,
			),
		}))
	})
})
