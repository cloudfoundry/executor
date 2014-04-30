package file_cache_test

import (
	"io"
	"io/ioutil"
	Url "net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudfoundry-incubator/executor/downloader/fake_downloader"
	"github.com/cloudfoundry-incubator/executor/file_cache"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("File cache", func() {
	var (
		cache             *file_cache.Cache
		basePath          string
		maxSizeInBytes    int
		downloader        *fake_downloader.FakeDownloader
		downloadedContent []byte
		url               *Url.URL
	)

	BeforeEach(func() {
		var err error
		url, err = Url.Parse("http://example.com")
		Ω(err).ShouldNot(HaveOccurred())

		basePath, err = ioutil.TempDir("", "test_file_cache")
		Ω(err).ShouldNot(HaveOccurred())

		maxSizeInBytes = 1024

		downloader = &fake_downloader.FakeDownloader{}

		cache = file_cache.New(basePath, maxSizeInBytes, downloader)
	})

	AfterEach(func() {
		os.RemoveAll(basePath)
	})

	var (
		file io.ReadCloser
		err  error
	)

	Describe("When providing a file with no cache key", func() {
		Context("when the download succeeds", func() {
			BeforeEach(func() {
				downloadedContent = []byte(strings.Repeat("7", maxSizeInBytes*3))
				downloader.DownloadContent = downloadedContent
				file, err = cache.Fetch(url, "")
			})

			It("should not error", func() {
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("should return a readCloser that streams the file", func() {
				Ω(file).ShouldNot(BeNil())
				Ω(ioutil.ReadAll(file)).Should(Equal(downloadedContent))
			})

			It("should delete the file when we close the readCloser", func() {
				Ω(ioutil.ReadDir(basePath)).Should(HaveLen(1))
				err := file.Close()
				Ω(err).ShouldNot(HaveOccurred())
				Ω(ioutil.ReadDir(basePath)).Should(HaveLen(0))
			})
		})

		Context("when the download fails", func() {
			BeforeEach(func() {
				downloader.AlwaysFail()
				file, err = cache.Fetch(url, "")
			})

			It("should return an error and no file", func() {
				Ω(file).Should(BeNil())
				Ω(err).Should(HaveOccurred())
			})

			It("should clean up after itself", func() {
				Ω(ioutil.ReadDir(basePath)).Should(HaveLen(0))
			})
		})
	})

	Describe("When providing a file with a cache key", func() {
		var cacheKey string = "E-sharp"

		Context("when the file is not in the cache", func() {
			Context("when the download succeeds", func() {
				BeforeEach(func() {
					downloadedContent = []byte(strings.Repeat("7", maxSizeInBytes/2))
					downloader.DownloadContent = downloadedContent
					file, err = cache.Fetch(url, cacheKey)
				})

				It("should not error", func() {
					Ω(err).ShouldNot(HaveOccurred())
				})

				It("should return a readCloser that streams the file", func() {
					Ω(file).ShouldNot(BeNil())
					Ω(ioutil.ReadAll(file)).Should(Equal(downloadedContent))
				})

				It("should return a file within the cache", func() {
					Ω(ioutil.ReadDir(basePath)).Should(HaveLen(1))
				})

				It("should not delete the file when we close the readCloser", func() {
					Ω(ioutil.ReadDir(basePath)).Should(HaveLen(1))
					err := file.Close()
					Ω(err).ShouldNot(HaveOccurred())
					Ω(ioutil.ReadDir(basePath)).Should(HaveLen(1))
				})
			})

			Context("when the download fails", func() {
				BeforeEach(func() {
					downloader.AlwaysFail()
					file, err = cache.Fetch(url, cacheKey)
				})

				It("should return an error and no file", func() {
					Ω(file).Should(BeNil())
					Ω(err).Should(HaveOccurred())
				})

				It("should clean up after itself", func() {
					Ω(ioutil.ReadDir(basePath)).Should(HaveLen(0))
				})
			})
		})

		Context("when the file is already on disk in the cache", func() {
			var cacheFilePath string
			var fileContent []byte
			BeforeEach(func() {
				cacheFilePath = filepath.Join(basePath, cacheKey)
				fileContent = []byte("now you see it")
				err := ioutil.WriteFile(cacheFilePath, fileContent, 0666)
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("should check for modifications", func() {
				cache.Fetch(url, cacheKey)
				Ω(downloader.ModifiedSinceURL).Should(Equal(url))

				file, err := os.Open(cacheFilePath)
				Ω(err).ShouldNot(HaveOccurred())

				fileInfo, err := file.Stat()
				Ω(err).ShouldNot(HaveOccurred())

				Ω(downloader.ModifiedSinceTime).Should(Equal(fileInfo.ModTime()))
			})

			Context("if the file has been modified", func() {
				BeforeEach(func() {
					downloader.IsModified = true
					downloader.DownloadContent = []byte("now you don't")
				})

				It("should redownload the file", func() {
					cache.Fetch(url, cacheKey)
					Ω(ioutil.ReadFile(cacheFilePath)).Should(Equal(downloader.DownloadContent))
				})

				It("should return a readcloser pointing to the file", func() {
					file, err := cache.Fetch(url, cacheKey)
					Ω(err).ShouldNot(HaveOccurred())
					Ω(ioutil.ReadAll(file)).Should(Equal(downloader.DownloadContent))
				})

				It("should not delete the file when closed", func() {
					file, err := cache.Fetch(url, cacheKey)
					Ω(err).ShouldNot(HaveOccurred())
					file.Close()
					Ω(ioutil.ReadDir(basePath)).Should(HaveLen(1))
				})
			})

			Context("if the file has not been modified", func() {
				BeforeEach(func() {
					downloader.IsModified = false
				})

				It("should not redownload the file", func() {
					_, err := cache.Fetch(url, cacheKey)
					Ω(err).ShouldNot(HaveOccurred())
					Ω(ioutil.ReadFile(cacheFilePath)).Should(Equal(fileContent))
				})

				It("should return a readcloser pointing to the file", func() {
					file, err := cache.Fetch(url, cacheKey)
					Ω(err).ShouldNot(HaveOccurred())
					Ω(ioutil.ReadAll(file)).Should(Equal(fileContent))
				})
			})
		})

		Context("when the file is too large for the cache", func() {

		})
	})
})
