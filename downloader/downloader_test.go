package downloader_test

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
	"time"

	. "github.com/cloudfoundry-incubator/executor/downloader"
	steno "github.com/cloudfoundry/gosteno"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

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
		var url *url.URL
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

			BeforeEach(func() {
				testServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					lock.Lock()
					serverRequestUrls = append(serverRequestUrls, r.RequestURI)
					lock.Unlock()

					bytesWritten, _ := fmt.Fprintln(w, "Hello, client")
					expectedBytes = int64(bytesWritten)
				}))

				serverUrl := testServer.URL + "/somepath"
				url, _ = url.Parse(serverUrl)
			})

			JustBeforeEach(func() {
				uploadedBytes, downloadErr = downloader.Download(url, file)
				Ω(downloadErr).ShouldNot(HaveOccurred())
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

		Context("when the download times out", func() {
			var requestInitiated chan struct{}

			BeforeEach(func() {
				requestInitiated = make(chan struct{})

				testServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requestInitiated <- struct{}{}

					time.Sleep(300 * time.Millisecond)
					fmt.Fprintln(w, "Hello, client")
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
	})
})
