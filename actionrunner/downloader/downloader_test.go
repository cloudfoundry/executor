package downloader_test

import (
	"fmt"
	. "github.com/cloudfoundry-incubator/executor/actionrunner/downloader"
	"os"
	"sync"
	"time"

	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Downloader", func() {
	var downloader Downloader
	var testServer *httptest.Server
	var serverRequestUrls []string
	var lock *sync.Mutex

	BeforeEach(func() {
		downloader = New(100 * time.Millisecond)
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
			testServer.Close()
		})

		Context("when the download is successful", func() {
			BeforeEach(func() {
				testServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					lock.Lock()
					serverRequestUrls = append(serverRequestUrls, r.RequestURI)
					lock.Unlock()
					fmt.Fprintln(w, "Hello, client")
				}))

				serverUrl := testServer.URL + "/somepath"
				url, _ = url.Parse(serverUrl)
			})

			JustBeforeEach(func() {
				err := downloader.Download(url, file)
				Ω(err).ShouldNot(HaveOccurred())
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
		})

		Context("when the download times out", func() {
			var attemptCount int
			BeforeEach(func() {
				attemptCount = 0
				testServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					lock.Lock()
					attemptCount++
					serverRequestUrls = append(serverRequestUrls, r.RequestURI)
					lock.Unlock()

					time.Sleep(300 * time.Millisecond)
					fmt.Fprintln(w, "Hello, client")
				}))

				serverUrl := testServer.URL + "/somepath"
				url, _ = url.Parse(serverUrl)
			})

			It("should retry 3 times", func() {
				downloader.Download(url, file)
				lock.Lock()
				Ω(attemptCount).Should(Equal(3))
				lock.Unlock()
			})

			It("should return an error", func() {
				err := downloader.Download(url, file)
				Ω(err).Should(HaveOccurred())
			})
		})

		Context("when the download fails with a protocol error", func() {
			BeforeEach(func() {
				// No server to handle things!

				serverUrl := testServer.URL + "/somepath"
				url, _ = url.Parse(serverUrl)
			})

			It("should return the error", func() {
				err := downloader.Download(url, file)
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
				err := downloader.Download(url, file)
				Ω(err).NotTo(BeNil())
			})
		})
	})
})
