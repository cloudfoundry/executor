package cacheddownloader_test

import (
	"crypto/md5"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	Url "net/url"
	"os"
	"sync"
	"time"

	"github.com/onsi/gomega/ghttp"

	. "github.com/cloudfoundry-incubator/executor/cacheddownloader"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func md5HexEtag(content string) string {
	contentHash := md5.New()
	contentHash.Write([]byte(content))
	return fmt.Sprintf(`"%x"`, contentHash.Sum(nil))
}

var _ = Describe("Downloader", func() {
	var downloader *Downloader
	var testServer *httptest.Server
	var serverRequestUrls []string
	var lock *sync.Mutex

	createDestFile := func() (*os.File, error) {
		return ioutil.TempFile("", "foo")
	}

	BeforeEach(func() {
		testServer = nil
		downloader = NewDownloader(100 * time.Millisecond)
		lock = &sync.Mutex{}
	})

	Describe("download", func() {
		var url *Url.URL

		BeforeEach(func() {
			serverRequestUrls = []string{}
		})

		AfterEach(func() {
			if testServer != nil {
				testServer.Close()
			}
		})

		Context("when the download is successful", func() {
			var (
				downloadSize int64
				expectedSize int64

				downloadErr    error
				downloadedFile string

				downloadCachingInfo CachingInfoType
				expectedCachingInfo CachingInfoType
				expectedEtag        string
			)

			AfterEach(func() {
				if downloadedFile != "" {
					os.Remove(downloadedFile)
				}
			})

			JustBeforeEach(func() {
				serverUrl := testServer.URL + "/somepath"
				url, _ = url.Parse(serverUrl)
				downloadedFile, downloadSize, downloadCachingInfo, downloadErr = downloader.Download(url, createDestFile, CachingInfoType{})
			})

			Context("and contains a matching MD5 Hash in the Etag", func() {
				var attempts int
				BeforeEach(func() {
					attempts = 0
					testServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						lock.Lock()
						serverRequestUrls = append(serverRequestUrls, r.RequestURI)
						attempts++
						lock.Unlock()

						msg := "Hello, client"
						expectedCachingInfo = CachingInfoType{
							ETag:         md5HexEtag(msg),
							LastModified: "The 70s",
						}
						w.Header().Set("ETag", expectedCachingInfo.ETag)
						w.Header().Set("Last-Modified", expectedCachingInfo.LastModified)

						bytesWritten, _ := fmt.Fprint(w, msg)
						expectedSize = int64(bytesWritten)
					}))
				})

				It("does not return an error", func() {
					Ω(downloadErr).ShouldNot(HaveOccurred())
				})

				It("only tries once", func() {
					Ω(attempts).Should(Equal(1))
				})

				It("claims to have downloaded", func() {
					Ω(downloadedFile).ShouldNot(BeEmpty())
				})

				It("gets a file from a url", func() {
					lock.Lock()
					urlFromServer := testServer.URL + serverRequestUrls[0]
					Ω(urlFromServer).To(Equal(url.String()))
					lock.Unlock()
				})

				It("should use the provided file as the download location", func() {
					fileContents, _ := ioutil.ReadFile(downloadedFile)
					Ω(fileContents).Should(ContainSubstring("Hello, client"))
				})

				It("return number of bytes it downloaded", func() {
					Ω(downloadSize).Should(Equal(expectedSize))
				})

				It("returns the ETag", func() {
					Ω(downloadCachingInfo).Should(Equal(expectedCachingInfo))
				})
			})

			Context("and contains an Etag that is not an MD5 Hash ", func() {
				BeforeEach(func() {
					expectedEtag = "not the hex you are looking for"
					testServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						w.Header().Set("ETag", expectedEtag)
						bytesWritten, _ := fmt.Fprint(w, "Hello, client")
						expectedSize = int64(bytesWritten)
					}))
				})

				It("succeeds without doing a checksum", func() {
					Ω(downloadedFile).ShouldNot(BeEmpty())
					Ω(downloadErr).ShouldNot(HaveOccurred())
				})

				It("should returns the ETag in the caching info", func() {
					Ω(downloadCachingInfo.ETag).Should(Equal(expectedEtag))
				})
			})

			Context("and contains no Etag at all", func() {
				BeforeEach(func() {
					testServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						bytesWritten, _ := fmt.Fprint(w, "Hello, client")
						expectedSize = int64(bytesWritten)
					}))
				})

				It("succeeds without doing a checksum", func() {
					Ω(downloadedFile).ShouldNot(BeEmpty())
					Ω(downloadErr).ShouldNot(HaveOccurred())
				})

				It("should returns no ETag in the caching info", func() {
					Ω(downloadCachingInfo).Should(BeZero())
				})
			})
		})

		Context("when the download times out", func() {
			var requestInitiated chan struct{}

			BeforeEach(func() {
				requestInitiated = make(chan struct{})

				testServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requestInitiated <- struct{}{}

					time.Sleep(300 * time.Millisecond)
					fmt.Fprint(w, "Hello, client")
				}))

				serverUrl := testServer.URL + "/somepath"
				url, _ = url.Parse(serverUrl)
			})

			It("should retry 3 times and return an error", func() {
				errs := make(chan error)
				downloadedFiles := make(chan string)

				go func() {
					downloadedFile, _, _, err := downloader.Download(url, createDestFile, CachingInfoType{})
					errs <- err
					downloadedFiles <- downloadedFile
				}()

				Eventually(requestInitiated).Should(Receive())
				Eventually(requestInitiated).Should(Receive())
				Eventually(requestInitiated).Should(Receive())

				Ω(<-errs).Should(HaveOccurred())
				Ω(<-downloadedFiles).Should(BeEmpty())
			})
		})

		Context("when the download fails with a protocol error", func() {
			BeforeEach(func() {
				// No server to handle things!

				serverUrl := "http://127.0.0.1:54321/somepath"
				url, _ = url.Parse(serverUrl)
			})

			It("should return the error", func() {
				downloadedFile, _, _, err := downloader.Download(url, createDestFile, CachingInfoType{})
				Ω(err).Should(HaveOccurred())
				Ω(downloadedFile).Should(BeEmpty())
			})
		})

		Context("when the download fails with a status code error", func() {
			BeforeEach(func() {
				testServer = httptest.NewServer(http.NotFoundHandler())

				serverUrl := testServer.URL + "/somepath"
				url, _ = url.Parse(serverUrl)
			})

			It("should return the error", func() {
				downloadedFile, _, _, err := downloader.Download(url, createDestFile, CachingInfoType{})
				Ω(err).Should(HaveOccurred())
				Ω(downloadedFile).Should(BeEmpty())
			})
		})

		Context("when the download's ETag fails the checksum", func() {
			BeforeEach(func() {
				testServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					realMsg := "Hello, client"
					incompleteMsg := "Hello, clien"

					w.Header().Set("ETag", md5HexEtag(realMsg))

					fmt.Fprint(w, incompleteMsg)
				}))

				serverUrl := testServer.URL + "/somepath"
				url, _ = url.Parse(serverUrl)
			})

			It("should return an error", func() {
				downloadedFile, _, cachingInfo, err := downloader.Download(url, createDestFile, CachingInfoType{})
				Ω(err).Should(HaveOccurred())
				Ω(downloadedFile).Should(BeEmpty())
				Ω(cachingInfo).Should(BeZero())
			})
		})
	})

	Context("Downloading witbh caching info", func() {
		var (
			server     *ghttp.Server
			cachedInfo CachingInfoType
			statusCode int
			url        *Url.URL
			body       string
		)

		BeforeEach(func() {
			cachedInfo = CachingInfoType{
				ETag:         "It's Just a Flesh Wound",
				LastModified: "The 60s",
			}

			server = ghttp.NewServer()
			server.AppendHandlers(ghttp.CombineHandlers(
				ghttp.VerifyRequest("GET", "/get-the-file"),
				ghttp.VerifyHeader(http.Header{
					"If-None-Match":     []string{cachedInfo.ETag},
					"If-Modified-Since": []string{cachedInfo.LastModified},
				}),
				ghttp.RespondWithPtr(&statusCode, &body),
			))

			url, _ = Url.Parse(server.URL() + "/get-the-file")
		})

		AfterEach(func() {
			server.Close()
		})

		Context("when the server replies with 304", func() {
			BeforeEach(func() {
				statusCode = http.StatusNotModified
			})

			It("should return that it did not download", func() {
				downloadedFile, size, _, err := downloader.Download(url, createDestFile, cachedInfo)
				Ω(downloadedFile).Should(BeEmpty())
				Ω(size).Should(Equal(int64(0)))
				Ω(err).ShouldNot(HaveOccurred())
			})
		})

		Context("when the server replies with 200", func() {
			var (
				downloadedFile string
				size           int64
				err            error
			)

			BeforeEach(func() {
				statusCode = http.StatusOK
				body = "quarb!"
			})

			AfterEach(func() {
				if downloadedFile != "" {
					os.Remove(downloadedFile)
				}
			})

			It("should return that it did download and the file size", func() {
				downloadedFile, size, _, err = downloader.Download(url, createDestFile, cachedInfo)
				Ω(downloadedFile).ShouldNot(BeEmpty())
				Ω(size).Should(Equal(int64(len(body))))
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("should download the file", func() {
				downloadedFile, _, _, _ = downloader.Download(url, createDestFile, cachedInfo)
				info, err := os.Stat(downloadedFile)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(info.Size()).Should(Equal(int64(len(body))))
			})
		})

		Context("for anything else (including a server error)", func() {
			BeforeEach(func() {
				statusCode = http.StatusInternalServerError

				// cope with built in retry
				for i := 0; i < MAX_DOWNLOAD_ATTEMPTS; i++ {
					server.AppendHandlers(ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", "/get-the-file"),
						ghttp.VerifyHeader(http.Header{
							"If-None-Match":     []string{cachedInfo.ETag},
							"If-Modified-Since": []string{cachedInfo.LastModified},
						}),
						ghttp.RespondWithPtr(&statusCode, &body),
					))
				}
			})

			It("should return false with an error", func() {
				downloadedFile, size, _, err := downloader.Download(url, createDestFile, cachedInfo)
				Ω(downloadedFile).Should(BeEmpty())
				Ω(size).Should(Equal(int64(0)))
				Ω(err).Should(HaveOccurred())
			})
		})
	})
})
