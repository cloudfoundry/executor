package downloader_test

import (
	"fmt"
	. "github.com/cloudfoundry-incubator/executor/actionrunner/downloader"
	"os"

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

	BeforeEach(func() {
		downloader = New()
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
					serverRequestUrls = append(serverRequestUrls, r.RequestURI)
					fmt.Fprintln(w, "Hello, client")
				}))

				serverUrl := testServer.URL + "/somepath"
				url, _ = url.Parse(serverUrl)
			})

			JustBeforeEach(func() {
				downloader.Download(url, file)
			})

			It("gets a file from a url", func() {
				urlFromServer := testServer.URL + serverRequestUrls[0]
				Ω(urlFromServer).To(Equal(url.String()))
			})

			It("should use the provided file as the download location", func() {
				fileContents, _ := ioutil.ReadFile(file.Name())
				Ω(fileContents).Should(ContainSubstring("Hello, client"))
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
