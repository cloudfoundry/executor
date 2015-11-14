package gardenhealth_test

import (
	"errors"

	"github.com/cloudfoundry-incubator/executor/depot/gardenstore"
	"github.com/cloudfoundry-incubator/executor/gardenhealth"
	"github.com/cloudfoundry-incubator/executor/guidgen/fakeguidgen"
	"github.com/cloudfoundry-incubator/garden"
	gardenFakes "github.com/cloudfoundry-incubator/garden/fakes"
	"github.com/pivotal-golang/lager/lagertest"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Checker", func() {
	const (
		rootfsPath         = "test-rootfs-path"
		containerOwnerName = "container-owner"
	)
	var (
		gardenChecker   gardenhealth.Checker
		gardenClient    *gardenFakes.FakeClient
		healthcheckSpec garden.ProcessSpec
		logger          *lagertest.TestLogger
	)

	BeforeEach(func() {
		healthcheckSpec = garden.ProcessSpec{
			Path: "/bin/sh",
			Args: []string{"-c", "echo", "hello"},
			User: "vcap",
		}
		logger = lagertest.NewTestLogger("test")
		gardenClient = &gardenFakes.FakeClient{}
		guidGenerator := &fakeguidgen.FakeGenerator{}
		guidGenerator.GuidReturns("abc-123")
		gardenChecker = gardenhealth.NewChecker(rootfsPath, containerOwnerName, healthcheckSpec, gardenClient, guidGenerator)
	})

	Describe("Healthcheck", func() {
		var fakeContainer *gardenFakes.FakeContainer
		var oldContainer *gardenFakes.FakeContainer
		var fakeProcess *gardenFakes.FakeProcess

		BeforeEach(func() {
			fakeContainer = &gardenFakes.FakeContainer{}
			oldContainer = &gardenFakes.FakeContainer{}
			oldContainer.HandleReturns("old-guid")
			fakeProcess = &gardenFakes.FakeProcess{}
		})

		Context("When garden is healthy", func() {
			BeforeEach(func() {
				gardenClient.CreateReturns(fakeContainer, nil)
				gardenClient.ContainersReturns([]garden.Container{oldContainer}, nil)
				fakeContainer.RunReturns(fakeProcess, nil)
				fakeProcess.WaitReturns(0, nil)
			})

			It("drives a container lifecycle", func() {
				err := gardenChecker.Healthcheck(logger)

				By("Fetching any pre-existing healthcheck containers")
				Expect(gardenClient.ContainersCallCount()).To(Equal(1))

				By("Deleting all pre-existing-containers")
				//call count is two because we also expect to destroy the container we create
				Expect(gardenClient.DestroyCallCount()).To(Equal(2))
				guid := gardenClient.DestroyArgsForCall(0)
				Expect(guid).To(Equal("old-guid"))

				By("Creates the container")
				Expect(gardenClient.CreateCallCount()).To(Equal(1))
				containerSpec := gardenClient.CreateArgsForCall(0)
				Expect(containerSpec).To(Equal(garden.ContainerSpec{
					Handle:     "executor-healthcheck-abc-123",
					RootFSPath: rootfsPath,
					Properties: garden.Properties{
						gardenstore.ContainerOwnerProperty: containerOwnerName,
						gardenhealth.HealthcheckTag:        gardenhealth.HealthcheckTagValue,
					},
				}))

				By("Runs the process")
				Expect(fakeContainer.RunCallCount()).To(Equal(1))

				procSpec, procIO := fakeContainer.RunArgsForCall(0)
				Expect(procSpec).To(Equal(healthcheckSpec))
				Expect(procIO).To(Equal(garden.ProcessIO{}))

				By("Waits for the process to finish")
				Expect(fakeProcess.WaitCallCount()).To(Equal(1))

				By("Destroys the container")
				guid = gardenClient.DestroyArgsForCall(1)
				Expect(guid).To(Equal("executor-healthcheck-abc-123"))

				By("Returns success")
				Expect(err).Should(BeNil())
			})
		})

		Context("when create fails", func() {
			var createErr = errors.New("nope")

			BeforeEach(func() {
				gardenClient.CreateReturns(nil, createErr)
			})

			It("sends back the creation error", func() {
				err := gardenChecker.Healthcheck(logger)
				Expect(err).To(Equal(createErr))
				Expect(gardenClient.DestroyCallCount()).To(Equal(0))
			})
		})

		Context("when run fails", func() {
			var runErr = errors.New("nope")

			BeforeEach(func() {
				gardenClient.CreateReturns(fakeContainer, nil)
				fakeContainer.RunReturns(nil, runErr)
			})

			It("sends back the run error", func() {
				err := gardenChecker.Healthcheck(logger)

				By("Sending the result back")
				Expect(err).To(Equal(runErr))

				By("Destroys the container")
				Expect(gardenClient.DestroyCallCount()).To(Equal(1))
			})
		})

		Context("when wait returns an error", func() {
			var waitErr = errors.New("no waiting!")

			BeforeEach(func() {
				gardenClient.CreateReturns(fakeContainer, nil)
				fakeContainer.RunReturns(fakeProcess, nil)
				fakeProcess.WaitReturns(0, waitErr)
			})

			It("sends back the wait error", func() {
				err := gardenChecker.Healthcheck(logger)
				Expect(err).To(Equal(waitErr))
			})
		})

		Context("when the health check process returns with a non-zero exit code", func() {
			BeforeEach(func() {
				gardenClient.CreateReturns(fakeContainer, nil)
				fakeContainer.RunReturns(fakeProcess, nil)
				fakeProcess.WaitReturns(1, nil)
			})

			It("sends back HealthcheckFailedError", func() {
				err := gardenChecker.Healthcheck(logger)
				Expect(err).To(Equal(gardenhealth.HealthcheckFailedError(1)))
			})
		})

		Context("when destroying the container fails", func() {
			var destroyErr = errors.New("no disassemble #5!")

			BeforeEach(func() {
				gardenClient.CreateReturns(fakeContainer, nil)
				fakeContainer.RunReturns(fakeProcess, nil)
				fakeProcess.WaitReturns(0, nil)
				gardenClient.DestroyReturns(destroyErr)
			})

			It("returns an UnrecoverableError", func() {
				err := gardenChecker.Healthcheck(logger)
				Expect(err).To(Equal(gardenhealth.UnrecoverableError(destroyErr.Error())))
			})
		})

		Context("when destroying fails to find the container", func() {
			var destroyErr = garden.ContainerNotFoundError{}

			BeforeEach(func() {
				gardenClient.CreateReturns(fakeContainer, nil)
				fakeContainer.RunReturns(fakeProcess, nil)
				fakeProcess.WaitReturns(0, nil)
				gardenClient.DestroyReturns(destroyErr)
			})

			It("does not typecast the error as UnrecoverableError", func() {
				err := gardenChecker.Healthcheck(logger)
				Expect(err).To(Equal(destroyErr))
			})
		})
	})
})
