package integration_test

import (
	"errors"
	"fmt"
	"net/http"
	"syscall"
	"testing"
	"time"

	"github.com/nu7hatch/gouuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/gexec"

	"github.com/cloudfoundry-incubator/executor/api"
	"github.com/cloudfoundry-incubator/executor/client"
	GardenServer "github.com/cloudfoundry-incubator/garden/server"
	"github.com/cloudfoundry-incubator/garden/warden"
	"github.com/cloudfoundry-incubator/garden/warden/fake_backend"
	"github.com/cloudfoundry-incubator/runtime-schema/models"

	"github.com/cloudfoundry-incubator/executor/integration/executor_runner"
)

func TestExecutorMain(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Integration Suite")
}

var executorPath string
var gardenServer *GardenServer.WardenServer
var runner *executor_runner.ExecutorRunner
var executorClient client.Client

var _ = BeforeSuite(func() {
	var err error
	executorPath, err = gexec.Build("github.com/cloudfoundry-incubator/executor", "-race")
	Ω(err).ShouldNot(HaveOccurred())
})

var _ = AfterSuite(func() {
	gexec.CleanupBuildArtifacts()
	if gardenServer != nil {
		gardenServer.Stop()
	}

	if runner != nil {
		runner.KillWithFire()
	}
})

var _ = Describe("Main", func() {
	var (
		wardenAddr string

		fakeBackend *fake_backend.FakeBackend
	)

	drainTimeout := 5 * time.Second
	aBit := drainTimeout / 5
	pruningInterval := time.Second

	BeforeEach(func() {
		var err error

		//gardenServer.Stop calls listener.Close()
		//apparently listener.Close() returns before the port is *actually* closed...?
		wardenPort := 9001 + CurrentGinkgoTestDescription().LineNumber
		executorAddr := fmt.Sprintf("127.0.0.1:%d", 1700+GinkgoParallelNode())

		executorClient = client.New(http.DefaultClient, "http://"+executorAddr)

		wardenAddr = fmt.Sprintf("127.0.0.1:%d", wardenPort)

		fakeBackend = fake_backend.New()
		gardenServer = GardenServer.New("tcp", wardenAddr, 0, fakeBackend)

		err = gardenServer.Start()
		Ω(err).ShouldNot(HaveOccurred())

		runner = executor_runner.New(
			executorPath,
			executorAddr,
			"tcp",
			wardenAddr,
			"",
			"",
		)
	})

	AfterEach(func() {
		runner.KillWithFire()
		gardenServer.Stop()
	})

	runTask := func(block bool) {
		fakeContainer := fake_backend.NewFakeContainer(warden.ContainerSpec{
			Properties: warden.Properties{
				"owner": runner.Config.ContainerOwnerName,
			},
		})

		streamChannel := make(chan warden.ProcessStream, 1)
		fakeContainer.StreamChannel = streamChannel
		if !block {
			exitStatus := uint32(0)
			streamChannel <- warden.ProcessStream{
				ExitStatus: &exitStatus,
			}
		}

		fakeBackend.CreateResult = fakeContainer

		guid, err := uuid.NewV4()
		Ω(err).ShouldNot(HaveOccurred())

		_, err = executorClient.AllocateContainer(guid.String(), api.ContainerAllocationRequest{
			MemoryMB: 1024,
			DiskMB:   1024,
		})
		Ω(err).ShouldNot(HaveOccurred())

		_, err = executorClient.InitializeContainer(guid.String(), api.ContainerInitializationRequest{})
		Ω(err).ShouldNot(HaveOccurred())

		err = executorClient.Run(guid.String(), api.ContainerRunRequest{
			Actions: []models.ExecutorAction{
				{Action: models.RunAction{Path: "ls"}},
			},
		})
		Ω(err).ShouldNot(HaveOccurred())
	}

	Describe("starting up", func() {
		Context("when there are containers that are owned by the executor", func() {
			var handleThatShouldDie string

			BeforeEach(func() {
				container, err := fakeBackend.Create(warden.ContainerSpec{
					Handle: "container-that-should-die",
					Properties: warden.Properties{
						"owner": "executor-name",
					},
				})
				Ω(err).ShouldNot(HaveOccurred())

				handleThatShouldDie = container.Handle()

				_, err = fakeBackend.Create(warden.ContainerSpec{
					Handle: "container-that-should-live",
				})
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("should delete those containers (and only those containers)", func() {
				runner.Start(executor_runner.Config{
					ContainerOwnerName: "executor-name",
				})

				Ω(fakeBackend.DestroyedContainers).Should(Equal([]string{handleThatShouldDie}))
			})

			Context("when deleting the container fails", func() {
				BeforeEach(func() {
					fakeBackend.SetDestroyError(errors.New("i tried to delete the thing but i failed. sorry."))
				})

				It("should exit sadly", func() {
					runner.StartWithoutCheck(executor_runner.Config{
						ContainerOwnerName: "executor-name",
					})

					Eventually(runner.Session).Should(gexec.Exit(1))
				})
			})
		})
	})

	Context("when started", func() {
		BeforeEach(func() {
			runner.Start(executor_runner.Config{
				DrainTimeout:            drainTimeout,
				RegistryPruningInterval: pruningInterval,
			})
		})

		Describe("pruning the registry", func() {
			It("should prune the registry periodically", func() {
				It("continually prunes the registry", func() {
					guid, err := uuid.NewV4()
					Ω(err).ShouldNot(HaveOccurred())

					_, err = executorClient.AllocateContainer(guid.String(), api.ContainerAllocationRequest{
						MemoryMB: 1024,
						DiskMB:   1024,
					})
					Ω(err).ShouldNot(HaveOccurred())

					Ω(executorClient.ListContainers).Should(HaveLen(1))
					Eventually(executorClient.ListContainers, pruningInterval*2).Should(BeEmpty())
				})
			})
		})

		Describe("when the executor receives the TERM signal", func() {
			It("stops all running tasks", func() {
				runTask(true)
				Eventually(pollContainers(fakeBackend)).Should(HaveLen(1))

				runner.Session.Terminate()

				Eventually(func() ([]warden.Container, error) {
					return fakeBackend.Containers(nil)
				}).Should(BeEmpty())

			})

			It("exits successfully", func() {
				runner.Session.Terminate()

				Eventually(runner.Session, 2*drainTimeout).Should(gexec.Exit(0))
			})
		})

		Describe("when the executor receives the INT signal", func() {
			It("stops all running tasks", func() {
				runTask(true)
				Eventually(pollContainers(fakeBackend)).Should(HaveLen(1))

				runner.Session.Interrupt()
				Eventually(pollContainers(fakeBackend)).Should(BeEmpty())
			})

			It("exits successfully", func() {
				runner.Session.Interrupt()
				Eventually(runner.Session, 2*drainTimeout).Should(gexec.Exit(0))
			})
		})

		Describe("when the executor receives the USR1 signal", func() {
			sendDrainSignal := func() {
				runner.Session.Signal(syscall.SIGUSR1)
				Eventually(runner.Session).Should(gbytes.Say("executor.draining"))
			}

			Context("when there are tasks running", func() {
				It("stops accepting requests", func() {
					sendDrainSignal()

					_, err := executorClient.AllocateContainer("container-123", api.ContainerAllocationRequest{})
					Ω(err).Should(HaveOccurred())
				})

				Context("and USR1 is received again", func() {
					It("does not die", func() {
						runTask(true)
						Eventually(pollContainers(fakeBackend)).Should(HaveLen(1))

						runner.Session.Signal(syscall.SIGUSR1)
						Eventually(runner.Session, time.Second).Should(gbytes.Say("executor.draining"))

						runner.Session.Signal(syscall.SIGUSR1)
						Eventually(runner.Session, 5*time.Second).Should(gbytes.Say("executor.signal.ignored"))
					})
				})

				Context("when the tasks complete before the drain timeout", func() {
					It("exits successfully", func() {
						runTask(false)
						Eventually(pollContainers(fakeBackend)).Should(HaveLen(1))

						sendDrainSignal()

						Eventually(runner.Session, drainTimeout-aBit).Should(gexec.Exit(0))
					})
				})

				Context("when the tasks do not complete before the drain timeout", func() {
					BeforeEach(func() {
						runTask(true)
						Eventually(pollContainers(fakeBackend)).Should(HaveLen(1))
					})

					It("cancels all running tasks", func() {
						sendDrainSignal()

						Eventually(pollContainers(fakeBackend), drainTimeout+aBit).Should(BeEmpty())
					})

					It("exits successfully", func() {
						sendDrainSignal()
						Eventually(runner.Session, 2*drainTimeout).Should(gexec.Exit(0))
					})
				})
			})

			Context("when there are no tasks running", func() {
				It("exits successfully", func() {
					sendDrainSignal()
					Eventually(runner.Session).Should(gexec.Exit(0))
				})
			})
		})
	})
})

func pollContainers(fakeBackend *fake_backend.FakeBackend) func() ([]warden.Container, error) {
	return func() ([]warden.Container, error) {
		return fakeBackend.Containers(nil)
	}
}
