package actionrunner_test

import (
	"archive/zip"
	"errors"
	"github.com/cloudfoundry-incubator/executor/actionrunner/downloader/fakedownloader"
	"io/ioutil"
	"os"
	"time"

	"code.google.com/p/gogoprotobuf/proto"
	. "github.com/cloudfoundry-incubator/executor/actionrunner"
	"github.com/cloudfoundry-incubator/executor/linuxplugin"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/vito/gordon/fake_gordon"
	"github.com/vito/gordon/warden"
)

var _ = Describe("ActionRunner", func() {
	var (
		actions     []models.ExecutorAction
		runner      *ActionRunner
		downloader  *fakedownloader.FakeDownloader
		gordon      *fake_gordon.FakeGordon
		linuxPlugin *linuxplugin.LinuxPlugin
	)

	var stream chan *warden.ProcessPayload

	BeforeEach(func() {
		gordon = fake_gordon.New()
		downloader = &fakedownloader.FakeDownloader{}
		linuxPlugin = linuxplugin.New()
		runner = New(gordon, linuxPlugin, downloader)
	})

	Describe("Running the RunAction", func() {
		BeforeEach(func() {
			actions = []models.ExecutorAction{
				{
					models.RunAction{
						Script: "sudo reboot",
						Env: map[string]string{
							"A": "1",
						},
					},
				},
			}

			stream = make(chan *warden.ProcessPayload, 1000)
			gordon.SetRunReturnValues(0, stream, nil)
		})

		Context("when the script succeeds", func() {
			BeforeEach(func() {
				stream <- &warden.ProcessPayload{ExitStatus: proto.Uint32(0)}
			})

			It("executes the command in the passed-in container", func() {
				err := runner.Run("handle-x", actions)
				Ω(err).ShouldNot(HaveOccurred())

				runningScript := gordon.ScriptsThatRan()[0]
				Ω(runningScript.Handle).Should(Equal("handle-x"))
				Ω(runningScript.Script).Should(Equal("export A=\"1\"\nsudo reboot"))
			})
		})

		Context("when gordon errors", func() {
			BeforeEach(func() {
				gordon.SetRunReturnValues(0, nil, errors.New("I, like, tried but failed"))
			})

			It("should return the error", func() {
				err := runner.Run("handle-x", actions)
				Ω(err).Should(Equal(errors.New("I, like, tried but failed")))
			})
		})

		Context("when the script has a non-zero exit code", func() {
			BeforeEach(func() {
				stream <- &warden.ProcessPayload{ExitStatus: proto.Uint32(19)}
			})

			It("should return an error with the exit code", func() {
				err := runner.Run("handle-x", actions)
				Ω(err.Error()).Should(ContainSubstring("19"))
			})
		})

		Context("when the action does not have a timeout", func() {
			It("does not enforce one (i.e. zero-value time.Duration)", func() {
				go func() {
					time.Sleep(100 * time.Millisecond)
					stream <- &warden.ProcessPayload{ExitStatus: proto.Uint32(0)}
				}()

				err := runner.Run("handle-x", actions)
				Ω(err).ShouldNot(HaveOccurred())
			})
		})

		Context("when the action has a timeout", func() {
			BeforeEach(func() {
				actions = []models.ExecutorAction{
					{
						models.RunAction{
							Script:  "sudo reboot",
							Timeout: 100 * time.Millisecond,
						},
					},
				}
			})

			Context("and the script completes in time", func() {
				It("succeeds", func() {
					stream <- &warden.ProcessPayload{ExitStatus: proto.Uint32(0)}

					err := runner.Run("handle-x", actions)
					Ω(err).ShouldNot(HaveOccurred())
				})
			})

			Context("and the script takes longer than the timeout", func() {
				It("returns a RunActionTimeoutError", func() {
					go func() {
						time.Sleep(1 * time.Second)
						stream <- &warden.ProcessPayload{ExitStatus: proto.Uint32(0)}
					}()

					err := runner.Run("handle-x", actions)
					Ω(err).Should(HaveOccurred())
					Ω(err).Should(Equal(RunActionTimeoutError{models.RunAction{
						Script:  "sudo reboot",
						Timeout: 100 * time.Millisecond,
					}}))
				})
			})
		})
	})

	Describe("Performing a DownloadAction, with feeling", func() {
		var err error

		BeforeEach(func() {
			actions = []models.ExecutorAction{
				{
					models.DownloadAction{
						From:    "http://mr_jones",
						To:      "/Antarctica",
						Extract: false,
					},
				},
			}
		})

		JustBeforeEach(func() {
			err = runner.Run("handle-x", actions)
		})

		It("should download the file from a URL", func() {
			Ω(downloader.DownloadedUrls[0].Host).To(ContainSubstring("mr_jones"))
		})

		It("should place the file in the container", func() {
			copied_file := gordon.ThingsCopiedIn()[0]
			Ω(copied_file.Dst).To(Equal("/Antarctica"))
		})

		Context("when there is an error downloading", func() {
			BeforeEach(func() {
				downloader.AlwaysFail() //and bring shame and dishonor to your house
			})

			It("should return the error", func() {
				Ω(err).ToNot(BeNil())
			})
		})

		Context("when the file needs extraction", func() {
			BeforeEach(func() {

				actions = []models.ExecutorAction{
					{
						models.DownloadAction{
							From:    "http://mr_jones",
							To:      "/Antarctica",
							Extract: true,
						},
					},
				}

				file, err := ioutil.TempFile(os.TempDir(), "test-zip")
				Ω(err).ShouldNot(HaveOccurred())

				zipWriter := zip.NewWriter(file)

				Ω(err).ShouldNot(HaveOccurred())
				firstFileWriter, _ := zipWriter.Create("first_file")
				firstFileWriter.Write([]byte("I"))
				secondFileWriter, _ := zipWriter.Create("directory/second_file")
				secondFileWriter.Write([]byte("love"))
				thirdFileWriter, _ := zipWriter.Create("directory/third_file")
				thirdFileWriter.Write([]byte("peaches"))

				err = zipWriter.Close()
				Ω(err).ShouldNot(HaveOccurred())

				downloader.SourceFile = file
			})

			It("should download the zipped file and send the contents to the container", func() {
				Ω(gordon.ThingsCopiedIn()[0].Dst).To(Equal("/Antarctica"))
				Ω(gordon.ThingsCopiedIn()[1].Dst).To(Equal("/Antarctica/directory"))
				Ω(gordon.ThingsCopiedIn()[2].Dst).To(Equal("/Antarctica/directory/second_file"))
				Ω(gordon.ThingsCopiedIn()[3].Dst).To(Equal("/Antarctica/directory/third_file"))
				Ω(gordon.ThingsCopiedIn()[4].Dst).To(Equal("/Antarctica/first_file"))
			})
		})
	})
})
