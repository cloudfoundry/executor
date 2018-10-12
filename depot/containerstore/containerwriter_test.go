package containerstore_test

import (
	"errors"
	"io/ioutil"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"code.cloudfoundry.org/executor/depot/containerstore"
	"code.cloudfoundry.org/garden/gardenfakes"
)

var _ = Describe("GardenContainerWriter", func() {
	var (
		gardenClient *gardenfakes.FakeClient
		container    *gardenfakes.FakeContainer
		process      *gardenfakes.FakeProcess
		writer       containerstore.GardenContainerWriter
		runError     error
	)

	BeforeEach(func() {
		gardenClient = new(gardenfakes.FakeClient)
		container = new(gardenfakes.FakeContainer)
		process = new(gardenfakes.FakeProcess)
		gardenClient.LookupReturns(container, nil)
		container.RunReturns(process, nil)
		process.WaitReturns(0, nil)
		writer = containerstore.GardenContainerWriter{
			Client: gardenClient,
		}
	})

	JustBeforeEach(func() {
		runError = writer.WriteFile("some-container", "/foo/bar/baz", []byte("some-stuff"))
	})

	It("succeeds", func() {
		Expect(runError).NotTo(HaveOccurred())
	})

	It("looks up the container", func() {
		Expect(gardenClient.LookupCallCount()).To(Equal(1))
		Expect(gardenClient.LookupArgsForCall(0)).To(Equal("some-container"))
	})

	Context("when container lookup fails", func() {
		BeforeEach(func() {
			gardenClient.LookupReturns(nil, errors.New("lookup-failure"))
		})

		It("returns the error", func() {
			Expect(runError).To(MatchError("lookup-failure"))
		})
	})

	It("runs an echo process in the container", func() {
		Expect(container.RunCallCount()).To(Equal(1))
		actualProcessSpec, actualIO := container.RunArgsForCall(0)

		Expect(actualProcessSpec.Path).To(Equal("/bin/sh"))
		Expect(len(actualProcessSpec.Args)).To(Equal(2))
		Expect(actualProcessSpec.Args[0]).To(Equal("-c"))
		Expect(actualProcessSpec.Args[1]).To(Equal("mkdir -p /foo/bar && cat <&0 > /foo/bar/baz"))

		stdinContent, err := ioutil.ReadAll(actualIO.Stdin)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(stdinContent)).To(Equal("some-stuff"))
	})

	Context("when running the process fails", func() {
		BeforeEach(func() {
			container.RunReturns(nil, errors.New("run-failure"))
		})

		It("returns the error", func() {
			Expect(runError).To(MatchError("run-failure"))
		})
	})

	It("waits for the process", func() {
		Expect(process.WaitCallCount()).To(Equal(1))
	})

	Context("when waiting for the process fails", func() {
		BeforeEach(func() {
			process.WaitReturns(0, errors.New("process-wait-failure"))
		})

		It("returns the error", func() {
			Expect(runError).To(MatchError("process-wait-failure"))
		})
	})

	Context("when the process exits with non-zero exit code", func() {
		BeforeEach(func() {
			process.WaitReturns(1234, nil)
		})

		It("returns an error", func() {
			Expect(runError).To(MatchError(ContainSubstring("process exited with non-zero code: 1234")))
		})
	})
})
