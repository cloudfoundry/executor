package fetch_result_step_test

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/pivotal-golang/lager/lagertest"

	garden_api "github.com/cloudfoundry-incubator/garden/api"
	"github.com/cloudfoundry-incubator/garden/client/fake_api_client"
	"github.com/cloudfoundry-incubator/runtime-schema/models"

	"github.com/cloudfoundry-incubator/executor/sequence"
	"github.com/cloudfoundry-incubator/executor/steps/emittable_error"
	. "github.com/cloudfoundry-incubator/executor/steps/fetch_result_step"
)

type ClosableBuffer struct {
	bytes.Buffer
	closed chan struct{}
}

func NewClosableBuffer() *ClosableBuffer {
	return &ClosableBuffer{closed: make(chan struct{})}
}

func (b *ClosableBuffer) Close() error {
	close(b.closed)
	return nil
}

func (b *ClosableBuffer) IsClosed() bool {
	select {
	case <-b.closed:
		return true
	default:
		return false
	}
}

var _ = Describe("FetchResultStep", func() {
	var (
		step              sequence.Step
		fetchResultAction models.FetchResultAction
		logger            *lagertest.TestLogger
		gardenClient      *fake_api_client.FakeClient
		result            string
	)

	handle := "some-container-handle"

	BeforeEach(func() {
		result = ""

		fetchResultAction = models.FetchResultAction{
			File: "/var/some-dir/foo",
		}

		logger = lagertest.NewTestLogger("test")

		gardenClient = fake_api_client.New()
	})

	JustBeforeEach(func() {
		container, err := gardenClient.Create(garden_api.ContainerSpec{
			Handle: handle,
		})
		Ω(err).ShouldNot(HaveOccurred())

		step = New(
			container,
			fetchResultAction,
			"/tmp",
			logger,
			&result,
		)
	})

	Context("when the file exists", func() {
		var buffer *ClosableBuffer
		BeforeEach(func() {
			gardenClient.Connection.StreamOutStub = func(handle, src string) (io.ReadCloser, error) {
				Ω(src).Should(Equal("/var/some-dir/foo"))

				buffer = NewClosableBuffer()
				tarWriter := tar.NewWriter(buffer)

				content := []byte("result content")

				err := tarWriter.WriteHeader(&tar.Header{
					Name: "foo",
					Size: int64(len(content)),
				})
				Ω(err).ShouldNot(HaveOccurred())

				_, err = tarWriter.Write(content)
				Ω(err).ShouldNot(HaveOccurred())

				return buffer, nil
			}
		})

		It("should return the contents of the file", func() {
			err := step.Perform()
			Ω(err).ShouldNot(HaveOccurred())

			Ω(result).Should(Equal("result content"))
			Ω(buffer.IsClosed()).Should(BeTrue())
		})
	})

	Context("when the file exists but is too large", func() {
		var buffer *ClosableBuffer
		BeforeEach(func() {
			// overflow the (hard-coded) file content limit of 10KB by 1 byte:
			gardenClient.Connection.StreamOutStub = func(handle, src string) (io.ReadCloser, error) {
				buffer = NewClosableBuffer()
				tarWriter := tar.NewWriter(buffer)

				tooBig := int64(1024*10 + 1)
				err := tarWriter.WriteHeader(&tar.Header{
					Name: "foo",
					Size: tooBig,
				})
				Ω(err).ShouldNot(HaveOccurred())

				_, err = tarWriter.Write(make([]byte, tooBig))
				Ω(err).ShouldNot(HaveOccurred())

				return buffer, nil
			}

		})

		It("should error", func() {
			err := step.Perform()
			Ω(err.Error()).Should(ContainSubstring("Copying out of the container failed"))
			Ω(err.Error()).Should(ContainSubstring("result file size exceeds limit"))

			Ω(result).Should(BeZero())
			Ω(buffer.IsClosed()).Should(BeTrue())
		})
	})

	Context("when the file does not exist", func() {
		disaster := errors.New("kaboom")

		BeforeEach(func() {
			gardenClient.Connection.StreamOutReturns(nil, disaster)
		})

		It("should return an error and an empty result", func() {
			err := step.Perform()
			Ω(err).Should(MatchError(emittable_error.New(disaster, "Copying out of the container failed")))

			Ω(result).Should(BeZero())
		})
	})
})
