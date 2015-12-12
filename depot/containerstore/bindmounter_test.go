package containerstore_test

import (
	"errors"
	"net/url"

	"github.com/cloudfoundry-incubator/cacheddownloader/cacheddownloaderfakes"
	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/executor/depot/containerstore"
	"github.com/cloudfoundry-incubator/executor/depot/log_streamer/fake_log_streamer"
	"github.com/cloudfoundry-incubator/garden"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
)

var _ = Describe("Bindmounter", func() {
	var (
		bindMounter containerstore.BindMounter
		cache       *cacheddownloaderfakes.FakeCachedDownloader
		mounts      []executor.BindMount
		logStreamer *fake_log_streamer.FakeLogStreamer
	)

	BeforeEach(func() {
		cache = &cacheddownloaderfakes.FakeCachedDownloader{}
		logStreamer = fake_log_streamer.NewFakeLogStreamer()
		bindMounter = containerstore.NewBindMounter(cache)
		mounts = []executor.BindMount{
			{Name: "name-1", CacheKey: "cache-key-1", LogSource: "log-source-1", From: "https://user:pass@example.com:8080/download-1", To: "/var/data/buildpack-1"},
			{Name: "name-2", CacheKey: "cache-key-2", LogSource: "log-source-2", From: "http://example.com:1515/download-2", To: "/var/data/buildpack-2"},
		}
	})

	Context("when fetching all of the mounts succeeds", func() {
		var bindMounts containerstore.BindMounts

		BeforeEach(func() {
			cache.FetchAsDirectoryReturns("/tmp/download/mounts", 123, nil)
			var err error
			bindMounts, err = bindMounter.DownloadBindMounts(logger, mounts, logStreamer)
			Expect(err).NotTo(HaveOccurred())
		})

		It("emits the download events", func() {
			stdout := logStreamer.Stdout().(*gbytes.Buffer)
			Expect(stdout.Contents()).To(ContainSubstring("Downloading name-1..."))
			Expect(stdout.Contents()).To(ContainSubstring("Downloaded name-1 (123B)"))
		})

		It("returns the expected mount information", func() {
			expectedGardenMounts := []garden.BindMount{
				{SrcPath: "/tmp/download/mounts", DstPath: "/var/data/buildpack-1", Mode: garden.BindMountModeRO, Origin: garden.BindMountOriginHost},
				{SrcPath: "/tmp/download/mounts", DstPath: "/var/data/buildpack-2", Mode: garden.BindMountModeRO, Origin: garden.BindMountOriginHost},
			}

			expectedCacheKeys := []containerstore.BindMountCacheKey{
				{CacheKey: "cache-key-1", Dir: "/tmp/download/mounts"},
				{CacheKey: "cache-key-2", Dir: "/tmp/download/mounts"},
			}

			Expect(bindMounts.GardenBindMounts).To(Equal(expectedGardenMounts))
			Expect(bindMounts.CacheKeys).To(Equal(expectedCacheKeys))
		})

		It("Downloads the directories", func() {
			Expect(cache.FetchAsDirectoryCallCount()).To(Equal(2))
			downloadUrl, cacheKey, _ := cache.FetchAsDirectoryArgsForCall(0)
			Expect(*downloadUrl).To(Equal(url.URL{Scheme: "https", Host: "example.com:8080", Path: "/download-1", User: url.UserPassword("user", "pass")}))
			Expect(cacheKey).To(Equal("cache-key-1"))

			downloadUrl, cacheKey, _ = cache.FetchAsDirectoryArgsForCall(1)
			Expect(*downloadUrl).To(Equal(url.URL{Scheme: "http", Host: "example.com:1515", Path: "/download-2"}))
			Expect(cacheKey).To(Equal("cache-key-2"))
		})
	})

	Context("When a mount has an invlid 'From' field", func() {
		BeforeEach(func() {
			mounts = []executor.BindMount{
				{Name: "name-1", CacheKey: "cache-key-1", LogSource: "log-source-1", From: "%", To: "/var/data/buildpack-1"},
			}
		})

		It("returns the error", func() {
			_, err := bindMounter.DownloadBindMounts(logger, mounts, logStreamer)
			Expect(err).To(HaveOccurred())
		})
	})

	Context("When fetching a directory fails", func() {
		BeforeEach(func() {
			cache.FetchAsDirectoryReturns("", 0, errors.New("nope"))
		})

		It("emits the download events", func() {
			_, _ = bindMounter.DownloadBindMounts(logger, mounts, logStreamer)
			stdout := logStreamer.Stdout().(*gbytes.Buffer)
			Expect(stdout.Contents()).To(ContainSubstring("Downloading name-1..."))
			Expect(stdout.Contents()).To(ContainSubstring("Downloading name-1 failed"))
		})

		It("returns the error", func() {
			_, err := bindMounter.DownloadBindMounts(logger, mounts, logStreamer)
			Expect(err).To(HaveOccurred())
		})
	})
})
