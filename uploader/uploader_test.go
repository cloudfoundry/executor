package uploader_test

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"sync"
	"time"

	. "github.com/cloudfoundry-incubator/executor/uploader"
	steno "github.com/cloudfoundry/gosteno"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Uploader", func() {
	var uploader Uploader
	var testServer *httptest.Server
	var serverRequests []*http.Request
	var serverRequestBody []string
	var lock *sync.Mutex

	BeforeEach(func() {
		testServer = nil
		serverRequestBody = []string{}
		serverRequests = []*http.Request{}
		uploader = New(100*time.Millisecond, steno.NewLogger("test-logger"))
		lock = &sync.Mutex{}
	})

	Describe("upload", func() {
		var url *url.URL
		var file *os.File

		BeforeEach(func() {
			file, _ = ioutil.TempFile("", "foo")
			file.WriteString("content that we can check later")
			file.Close()
		})

		AfterEach(func() {
			file.Close()
			if testServer != nil {
				testServer.Close()
			}
		})

		Context("when the upload is successful", func() {
			BeforeEach(func() {
				testServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					serverRequests = append(serverRequests, r)

					data, err := ioutil.ReadAll(r.Body)
					Ω(err).ShouldNot(HaveOccurred())
					serverRequestBody = append(serverRequestBody, string(data))

					fmt.Fprintln(w, "Hello, client")
				}))

				serverUrl := testServer.URL + "/somepath"
				url, _ = url.Parse(serverUrl)
			})

			JustBeforeEach(func() {
				uploader.Upload(file.Name(), url)
			})

			It("uploads the file to the url", func() {
				Ω(serverRequests).Should(HaveLen(1))
				request := serverRequests[0]
				data := serverRequestBody[0]

				Ω(request.URL.Path).Should(Equal("/somepath"))
				Ω(request.Header.Get("Content-Type")).Should(Equal("application/octet-stream"))
				Ω(strconv.Atoi(request.Header.Get("Content-Length"))).Should(BeNumerically("==", 31))
				Ω(string(data)).Should(Equal("content that we can check later"))
			})
		})

		Context("when the upload times out", func() {
			var attemptCount int
			BeforeEach(func() {
				attemptCount = 0
				testServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					lock.Lock()
					attemptCount++
					lock.Unlock()

					time.Sleep(300 * time.Millisecond)
					fmt.Fprintln(w, "Hello, client")
				}))

				serverUrl := testServer.URL + "/somepath"
				url, _ = url.Parse(serverUrl)
			})

			It("should retry 3 times", func() {
				uploader.Upload(file.Name(), url)
				lock.Lock()
				Ω(attemptCount).Should(Equal(3))
				lock.Unlock()
			})

			It("should return an error", func() {
				err := uploader.Upload(file.Name(), url)
				Ω(err).Should(HaveOccurred())
			})
		})

		Context("when the upload fails with a protocol error", func() {
			BeforeEach(func() {
				// No server to handle things!

				serverUrl := "http://127.0.0.1:54321/somepath"
				url, _ = url.Parse(serverUrl)
			})

			It("should return the error", func() {
				err := uploader.Upload(file.Name(), url)
				Ω(err).NotTo(BeNil())
			})
		})

		Context("when the upload fails with a status code error", func() {
			BeforeEach(func() {
				testServer = httptest.NewServer(http.NotFoundHandler())

				serverUrl := testServer.URL + "/somepath"
				url, _ = url.Parse(serverUrl)
			})

			It("should return the error", func() {
				err := uploader.Upload(file.Name(), url)
				Ω(err).NotTo(BeNil())
			})
		})
	})
})
