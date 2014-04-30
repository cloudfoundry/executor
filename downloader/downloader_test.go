package downloader_test

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

	. "github.com/cloudfoundry-incubator/executor/downloader"
	steno "github.com/cloudfoundry/gosteno"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func md5HexEtag(content string) string {
	contentHash := md5.New()
	contentHash.Write([]byte(content))
	return fmt.Sprintf(`"%x"`, contentHash.Sum(nil))
}

var _ = Describe("Downloader", func() {
	var downloader Downloader
	var testServer *httptest.Server
	var serverRequestUrls []string
	var lock *sync.Mutex

	BeforeEach(func() {
		testServer = nil
		downloader = New(100*time.Millisecond, steno.NewLogger("test-logger"))
		lock = &sync.Mutex{}
	})

	Describe("download", func() {
		var url *Url.URL
		var file *os.File

		BeforeEach(func() {
			serverRequestUrls = []string{}
			file, _ = ioutil.TempFile("", "foo")
		})

		AfterEach(func() {
			file.Close()

			if testServer != nil {
				testServer.Close()
			}
		})

		Context("when the download is successful", func() {
			var uploadedBytes int64
			var expectedBytes int64
			var downloadErr error

			JustBeforeEach(func() {
				serverUrl := testServer.URL + "/somepath"
				url, _ = url.Parse(serverUrl)
				uploadedBytes, downloadErr = downloader.Download(url, file)
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
						w.Header().Set("ETag", md5HexEtag(msg))

						bytesWritten, _ := fmt.Fprint(w, msg)
						expectedBytes = int64(bytesWritten)
					}))
				})

				It("does not return an error", func() {
					Ω(downloadErr).ShouldNot(HaveOccurred())
				})

				It("only tries once", func() {
					Ω(attempts).Should(Equal(1))
				})

				It("gets a file from a url", func() {
					lock.Lock()
					urlFromServer := testServer.URL + serverRequestUrls[0]
					Ω(urlFromServer).To(Equal(url.String()))
					lock.Unlock()
				})

				It("should use the provided file as the download location", func() {
					fileContents, _ := ioutil.ReadFile(file.Name())
					Ω(fileContents).Should(ContainSubstring("Hello, client"))
				})

				It("return number of bytes it downloaded", func() {
					Ω(uploadedBytes).Should(Equal(expectedBytes))
				})
			})

			Context("and contains an Etag that is not an MD5 Hash ", func() {
				BeforeEach(func() {
					testServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						w.Header().Set("ETag", "not the hex you are looking for")
						bytesWritten, _ := fmt.Fprint(w, "Hello, client")
						expectedBytes = int64(bytesWritten)
					}))
				})

				It("succeeds without doing a checksum", func() {
					Ω(downloadErr).ShouldNot(HaveOccurred())
				})
			})

			Context("and contains no Etag at all", func() {
				BeforeEach(func() {
					testServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						bytesWritten, _ := fmt.Fprint(w, "Hello, client")
						expectedBytes = int64(bytesWritten)
					}))
				})

				It("succeeds without doing a checksum", func() {
					Ω(downloadErr).ShouldNot(HaveOccurred())
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

				go func() {
					_, err := downloader.Download(url, file)
					errs <- err
				}()

				Eventually(requestInitiated).Should(Receive())
				Eventually(requestInitiated).Should(Receive())
				Eventually(requestInitiated).Should(Receive())

				Ω(<-errs).Should(HaveOccurred())
			})
		})

		Context("when the download fails with a protocol error", func() {
			BeforeEach(func() {
				// No server to handle things!

				serverUrl := "http://127.0.0.1:54321/somepath"
				url, _ = url.Parse(serverUrl)
			})

			It("should return the error", func() {
				_, err := downloader.Download(url, file)
				Ω(err).NotTo(BeNil())
			})
		})

		Context("when the download fails with a status code error", func() {
			BeforeEach(func() {
				testServer = httptest.NewServer(http.NotFoundHandler())

				serverUrl := testServer.URL + "/somepath"
				url, _ = url.Parse(serverUrl)
			})

			It("should return the error", func() {
				_, err := downloader.Download(url, file)
				Ω(err).NotTo(BeNil())
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
				_, err := downloader.Download(url, file)
				Ω(err).NotTo(BeNil())
			})
		})
	})

	Describe("ModifiedSince", func() {
		var (
			server       *ghttp.Server
			modifiedTime time.Time
			statusCode   int
			url          *Url.URL
		)

		BeforeEach(func() {
			statusCode = http.StatusNotModified
			modifiedTime = time.Now()
			server = ghttp.NewServer()
			server.AppendHandlers(ghttp.CombineHandlers(
				ghttp.VerifyRequest("HEAD", "/get-the-file"),
				ghttp.VerifyHeader(http.Header{"If-Modified-Since": []string{modifiedTime.Format(http.TimeFormat)}}),
				ghttp.RespondWithPtr(&statusCode, nil),
			))

			url, _ = Url.Parse(server.URL() + "/get-the-file")
		})

		AfterEach(func() {
			server.Close()
		})

		It("should perform a HEAD request with the correct If-Modified-Since header", func() {
			downloader.ModifiedSince(url, modifiedTime)
			Ω(server.ReceivedRequests()).Should(HaveLen(1))
		})

		Context("when the server returns 304", func() {
			BeforeEach(func() {
				statusCode = http.StatusNotModified
			})

			It("should return false", func() {
				isModified, err := downloader.ModifiedSince(url, modifiedTime)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(isModified).Should(BeFalse())
			})
		})

		Context("when the server returns 200", func() {
			BeforeEach(func() {
				statusCode = http.StatusOK
			})

			It("should return true", func() {
				isModified, err := downloader.ModifiedSince(url, modifiedTime)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(isModified).Should(BeTrue())
			})
		})

		Context("for anything else (including a server error)", func() {
			BeforeEach(func() {
				statusCode = http.StatusInternalServerError
			})

			It("should return false with an error", func() {
				isModified, err := downloader.ModifiedSince(url, modifiedTime)
				Ω(err).Should(HaveOccurred())
				Ω(isModified).Should(BeFalse())
			})
		})
	})
})
