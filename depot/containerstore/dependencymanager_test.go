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

var _ = Describe("DependencyManager", func() {
	var (
		dependencyManager containerstore.DependencyManager
		cache             *cacheddownloaderfakes.FakeCachedDownloader
		dependencies      []executor.CachedDependency
		logStreamer       *fake_log_streamer.FakeLogStreamer
	)

	BeforeEach(func() {
		cache = &cacheddownloaderfakes.FakeCachedDownloader{}
		logStreamer = fake_log_streamer.NewFakeLogStreamer()
		dependencyManager = containerstore.NewDependencyManager(cache)
		dependencies = []executor.CachedDependency{
			{Name: "name-1", CacheKey: "cache-key-1", LogSource: "log-source-1", From: "https://user:pass@example.com:8080/download-1", To: "/var/data/buildpack-1"},
			{CacheKey: "cache-key-2", LogSource: "log-source-2", From: "http://example.com:1515/download-2", To: "/var/data/buildpack-2"},
		}
	})

	Context("when fetching all of the dependencies succeeds", func() {
		var bindMounts containerstore.BindMounts

		BeforeEach(func() {
			cache.FetchAsDirectoryReturns("/tmp/download/dependencies", 123, nil)
			var err error
			bindMounts, err = dependencyManager.DownloadCachedDependencies(logger, dependencies, logStreamer)
			Expect(err).NotTo(HaveOccurred())
		})

		It("uses the log source of the cached dependency", func() {
			Expect(logStreamer.WithSourceCallCount()).To(Equal(2))
			expectedSources := []string{
				"log-source-1",
				"log-source-2",
			}

			Expect(expectedSources).To(ContainElement(logStreamer.WithSourceArgsForCall(0)))
			Expect(expectedSources).To(ContainElement(logStreamer.WithSourceArgsForCall(1)))
		})

		It("emits the download log messages for downloads with names", func() {
			stdout := logStreamer.Stdout().(*gbytes.Buffer)
			Expect(stdout.Contents()).To(ContainSubstring("Downloading name-1..."))
			Expect(stdout.Contents()).To(ContainSubstring("Downloaded name-1 (123B)"))

			Expect(stdout.Contents()).ToNot(ContainSubstring("Downloading ..."))
			Expect(stdout.Contents()).ToNot(ContainSubstring("Downloaded  (123B)"))
		})

		It("returns the expected mount information", func() {
			expectedGardenMounts := []garden.BindMount{
				{SrcPath: "/tmp/download/dependencies", DstPath: "/var/data/buildpack-1", Mode: garden.BindMountModeRO, Origin: garden.BindMountOriginHost},
				{SrcPath: "/tmp/download/dependencies", DstPath: "/var/data/buildpack-2", Mode: garden.BindMountModeRO, Origin: garden.BindMountOriginHost},
			}

			expectedCacheKeys := []containerstore.BindMountCacheKey{
				{CacheKey: "cache-key-1", Dir: "/tmp/download/dependencies"},
				{CacheKey: "cache-key-2", Dir: "/tmp/download/dependencies"},
			}

			Expect(bindMounts.GardenBindMounts).To(ConsistOf(expectedGardenMounts))
			Expect(bindMounts.CacheKeys).To(ConsistOf(expectedCacheKeys))
		})

		It("Downloads the directories", func() {
			Expect(cache.FetchAsDirectoryCallCount()).To(Equal(2))
			// Again order here will not necessisarily be preserved!
			expectedUrls := []url.URL{
				{Scheme: "https", Host: "example.com:8080", Path: "/download-1", User: url.UserPassword("user", "pass")},
				{Scheme: "http", Host: "example.com:1515", Path: "/download-2"},
			}
			expectedCacheKeys := []string{
				"cache-key-1",
				"cache-key-2",
			}

			downloadURLs := make([]url.URL, 2)
			cacheKeys := make([]string, 2)
			downloadUrl, cacheKey, _ := cache.FetchAsDirectoryArgsForCall(0)
			downloadURLs[0] = *downloadUrl
			cacheKeys[0] = cacheKey
			downloadUrl, cacheKey, _ = cache.FetchAsDirectoryArgsForCall(1)
			downloadURLs[1] = *downloadUrl
			cacheKeys[1] = cacheKey
			Expect(downloadURLs).To(ConsistOf(expectedUrls))
			Expect(cacheKeys).To(ConsistOf(expectedCacheKeys))
		})
	})

	Context("When a mount has an invalid 'From' field", func() {
		BeforeEach(func() {
			dependencies = []executor.CachedDependency{
				{Name: "name-1", CacheKey: "cache-key-1", LogSource: "log-source-1", From: "%", To: "/var/data/buildpack-1"},
			}
		})

		It("returns the error", func() {
			_, err := dependencyManager.DownloadCachedDependencies(logger, dependencies, logStreamer)
			Expect(err).To(HaveOccurred())
		})
	})

	Context("When fetching a directory fails", func() {
		BeforeEach(func() {
			cache.FetchAsDirectoryReturns("", 0, errors.New("nope"))
		})

		It("emits the download events", func() {
			_, _ = dependencyManager.DownloadCachedDependencies(logger, dependencies, logStreamer)
			Eventually(func() []byte {
				stdout := logStreamer.Stdout().(*gbytes.Buffer)
				return stdout.Contents()
			}).Should(And(ContainSubstring("Downloading name-1..."), ContainSubstring("Downloading name-1 failed")))
		})

		It("returns the error", func() {
			_, err := dependencyManager.DownloadCachedDependencies(logger, dependencies, logStreamer)
			Expect(err).To(HaveOccurred())
		})
	})

	Context("When there are no cached dependencies ", func() {
		It("returns an empty list of bindmounts", func() {
			bindMounts, err := dependencyManager.DownloadCachedDependencies(logger, nil, logStreamer)
			Expect(err).NotTo(HaveOccurred())
			Expect(bindMounts.CacheKeys).To(HaveLen(0))
			Expect(bindMounts.GardenBindMounts).To(HaveLen(0))
		})
	})
})
