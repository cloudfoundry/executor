package file_cache_test

import (
	"crypto/md5"
	"fmt"
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

func computeMd5(key string) string {
	return fmt.Sprintf("%x", md5.Sum([]byte(key)))
}

var _ = Describe("File cache", func() {
	var (
		cache             *file_cache.Cache
		cachedPath        string
		uncachedPath      string
		maxSizeInBytes    int64
		downloader        *fake_downloader.FakeDownloader
		downloadedContent []byte
		url               *Url.URL
	)

	BeforeEach(func() {
		var err error
		url, err = Url.Parse("http://example.com")
		Ω(err).ShouldNot(HaveOccurred())

		cachedPath, err = ioutil.TempDir("", "test_file_cached")
		Ω(err).ShouldNot(HaveOccurred())

		uncachedPath, err = ioutil.TempDir("", "test_file_uncached")
		Ω(err).ShouldNot(HaveOccurred())

		maxSizeInBytes = 1024

		downloader = &fake_downloader.FakeDownloader{}

		cache = file_cache.New(cachedPath, uncachedPath, maxSizeInBytes, downloader)
	})

	AfterEach(func() {
		os.RemoveAll(cachedPath)
		os.RemoveAll(uncachedPath)
	})

	var (
		file io.ReadCloser
		err  error
	)

	Describe("when the cache folder does not exist", func() {
		It("should create it", func() {
			os.RemoveAll(cachedPath)
			cache = file_cache.New(cachedPath, uncachedPath, maxSizeInBytes, downloader)
			_, err := os.Stat(cachedPath)
			Ω(err).ShouldNot(HaveOccurred())
		})
	})

	Describe("when the cache folder has stuff in it", func() {
		It("should nuke that stuff", func() {
			filename := filepath.Join(cachedPath, "last_nights_dinner")
			ioutil.WriteFile(filename, []byte("leftovers"), 0666)
			cache = file_cache.New(cachedPath, uncachedPath, maxSizeInBytes, downloader)
			_, err := os.Stat(filename)
			Ω(err).Should(HaveOccurred())
		})
	})

	Describe("When providing a file with no cache key", func() {
		Context("when the download succeeds", func() {
			BeforeEach(func() {
				downloadedContent = []byte(strings.Repeat("7", int(maxSizeInBytes*3)))
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
				err := file.Close()
				Ω(err).ShouldNot(HaveOccurred())
				Ω(ioutil.ReadDir(uncachedPath)).Should(HaveLen(0))
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
				Ω(ioutil.ReadDir(uncachedPath)).Should(HaveLen(0))
			})
		})
	})

	Describe("When providing a file with a cache key", func() {
		var cacheKey string = "E-sharp"

		Context("when the file is not in the cache", func() {
			Context("when the download succeeds", func() {
				BeforeEach(func() {
					downloadedContent = []byte(strings.Repeat("7", int(maxSizeInBytes/2)))
					downloader.DownloadContent = downloadedContent
					file, err = cache.Fetch(url, cacheKey)
				})

				It("should request the download with a zero modified time", func() {
					Ω(downloader.ModifiedSinceTime).Should(BeZero())
				})

				It("should not error", func() {
					Ω(err).ShouldNot(HaveOccurred())
				})

				It("should return a readCloser that streams the file", func() {
					Ω(file).ShouldNot(BeNil())
					Ω(ioutil.ReadAll(file)).Should(Equal(downloadedContent))
				})

				It("should return a file within the cache", func() {
					Ω(ioutil.ReadDir(cachedPath)).Should(HaveLen(1))
				})

				It("should remove any temporary assets generated along the way", func() {
					Ω(ioutil.ReadDir(uncachedPath)).Should(HaveLen(0))
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
					Ω(ioutil.ReadDir(cachedPath)).Should(HaveLen(0))
					Ω(ioutil.ReadDir(uncachedPath)).Should(HaveLen(0))
				})
			})
		})

		Context("when the file is already on disk in the cache", func() {
			var cacheFilePath string
			var fileContent []byte
			BeforeEach(func() {
				cacheFilePath = filepath.Join(cachedPath, computeMd5(cacheKey))
				fileContent = []byte("now you see it")
				err := ioutil.WriteFile(cacheFilePath, fileContent, 0666)
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("should check for modifications", func() {
				cache.Fetch(url, cacheKey)

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

				It("should have put the file in the cache", func() {
					_, err := cache.Fetch(url, cacheKey)
					Ω(err).ShouldNot(HaveOccurred())
					Ω(ioutil.ReadDir(cachedPath)).Should(HaveLen(1))
					Ω(ioutil.ReadDir(uncachedPath)).Should(HaveLen(0))
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

		Context("when the file size exceeds the total available cache size", func() {
			BeforeEach(func() {
				downloadedContent = []byte(strings.Repeat("7", int(maxSizeInBytes*3)))
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

			It("should put the file in the uncached path, then delete it", func() {
				err := file.Close()
				Ω(err).ShouldNot(HaveOccurred())
				Ω(ioutil.ReadDir(cachedPath)).Should(HaveLen(0))
				Ω(ioutil.ReadDir(uncachedPath)).Should(HaveLen(0))
			})
		})

		Context("when the cache key has weird characters", func() {
			It("shouldn't miss a beat", func() {
				cacheKey = "http://mwahahaha/foo.com:c:/rm -rf"
				_, err := cache.Fetch(url, cacheKey)
				Ω(err).ShouldNot(HaveOccurred())
				dir, err := ioutil.ReadDir(cachedPath)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(dir[0].Name()).Should(Equal(computeMd5(cacheKey)))
			})
		})

		Context("when the cache is full", func() {
			It("deletes the oldest cached files until there is space", func() {
				//read them one at a time
				downloader.DownloadContent = []byte(strings.Repeat("C", int(maxSizeInBytes/4)))
				cachedFile1, err := cache.Fetch(url, "C")
				Ω(err).ShouldNot(HaveOccurred())
				cachedFile1.Close()

				downloader.DownloadContent = []byte(strings.Repeat("A", int(maxSizeInBytes/4)))
				cachedFile2, err := cache.Fetch(url, "A")
				Ω(err).ShouldNot(HaveOccurred())
				cachedFile2.Close()

				downloader.DownloadContent = []byte(strings.Repeat("B", int(maxSizeInBytes/4)))
				cachedFile3, err := cache.Fetch(url, "B")
				Ω(err).ShouldNot(HaveOccurred())
				cachedFile3.Close()

				cachedFile1, err = cache.Fetch(url, "C")
				Ω(err).ShouldNot(HaveOccurred())
				cachedFile1.Close()

				//try to add a file that has size 513
				downloadedContent = []byte(strings.Repeat("D", int(maxSizeInBytes/2)+1))
				downloader.DownloadContent = downloadedContent

				cachedFile4, err := cache.Fetch(url, "D")
				Ω(err).ShouldNot(HaveOccurred())
				Ω(ioutil.ReadAll(cachedFile4)).Should(Equal(downloadedContent))

				//make sure we removed the two we read first
				Ω(ioutil.ReadDir(cachedPath)).Should(HaveLen(2))

				_, err = os.Stat(filepath.Join(cachedPath, computeMd5("A")))
				Ω(err).Should(HaveOccurred())
				_, err = os.Stat(filepath.Join(cachedPath, computeMd5("B")))
				Ω(err).Should(HaveOccurred())

				_, err = os.Stat(filepath.Join(cachedPath, computeMd5("C")))
				Ω(err).ShouldNot(HaveOccurred())
				_, err = os.Stat(filepath.Join(cachedPath, computeMd5("D")))
				Ω(err).ShouldNot(HaveOccurred())
			})
		})
	})
})
