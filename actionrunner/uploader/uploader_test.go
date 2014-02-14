package uploader_test

import (
	"fmt"
	. "github.com/cloudfoundry-incubator/executor/actionrunner/uploader"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"os"

	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
)

var _ = Describe("Uploader", func() {
	var uploader Uploader
	var testServer *httptest.Server
	var serverRequests []*http.Request
	var serverRequestBody []string

	BeforeEach(func() {
		serverRequestBody = []string{}
		serverRequests = []*http.Request{}
		uploader = New()
	})

	Describe("upload", func() {
		var url *url.URL
		var file *os.File

		BeforeEach(func() {
			file, _ = ioutil.TempFile("", "foo")
			file.WriteString("content that we can check later")
			file.Seek(0, 0)
		})

		AfterEach(func() {
			file.Close()
			testServer.Close()
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
				uploader.Upload(file, url)
			})

			It("uploads the file to the url", func() {
				Ω(serverRequests).Should(HaveLen(1))
				request := serverRequests[0]
				data := serverRequestBody[0]

				Ω(request.URL.Path).Should(Equal("/somepath"))
				Ω(request.Header.Get("Content-Type")).Should(Equal("application/octet-stream"))
				Ω(string(data)).Should(Equal("content that we can check later"))
			})
		})

		Context("when the upload fails with a protocol error", func() {
			BeforeEach(func() {
				// No server to handle things!

				serverUrl := testServer.URL + "/somepath"
				url, _ = url.Parse(serverUrl)
			})

			It("should return the error", func() {
				err := uploader.Upload(file, url)
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
				err := uploader.Upload(file, url)
				Ω(err).NotTo(BeNil())
			})
		})
	})
})
