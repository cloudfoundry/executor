package integration_test

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"reflect"
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
	wfakes "github.com/cloudfoundry-incubator/garden/warden/fakes"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/loggregatorlib/logmessage"

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
})

var _ = Describe("Main", func() {
	var (
		wardenAddr string

		fakeBackend *wfakes.FakeBackend

		logMessages      <-chan *logmessage.LogMessage
		logConfirmations <-chan struct{}
	)

	drainTimeout := 5 * time.Second
	aBit := drainTimeout / 5
	pruningInterval := time.Second

	eventuallyContainerShouldBeDestroyed := func(args ...interface{}) {
		Eventually(fakeBackend.DestroyCallCount, args...).Should(Equal(1))
		handle := fakeBackend.DestroyArgsForCall(0)
		Ω(handle).Should(Equal("some-handle"))
	}

	BeforeEach(func() {
		var err error

		//gardenServer.Stop calls listener.Close()
		//apparently listener.Close() returns before the port is *actually* closed...?
		wardenPort := 9001 + CurrentGinkgoTestDescription().LineNumber
		executorAddr := fmt.Sprintf("127.0.0.1:%d", 1700+GinkgoParallelNode())

		executorClient = client.New(http.DefaultClient, "http://"+executorAddr)

		wardenAddr = fmt.Sprintf("127.0.0.1:%d", wardenPort)

		fakeBackend = new(wfakes.FakeBackend)
		fakeBackend.CapacityReturns(warden.Capacity{
			MemoryInBytes: 1024 * 1024 * 1024,
			DiskInBytes:   1024 * 1024 * 1024,
			MaxContainers: 1024,
		}, nil)

		gardenServer = GardenServer.New("tcp", wardenAddr, 0, fakeBackend)

		err = gardenServer.Start()
		Ω(err).ShouldNot(HaveOccurred())

		var loggregatorAddr string

		bidirectionalLogConfs := make(chan struct{})
		logConfirmations = bidirectionalLogConfs
		loggregatorAddr, logMessages = startFakeLoggregatorServer(bidirectionalLogConfs)

		runner = executor_runner.New(
			executorPath,
			executorAddr,
			"tcp",
			wardenAddr,
			loggregatorAddr,
			"the-loggregator-secret",
		)
	})

	AfterEach(func() {
		runner.KillWithFire()
		gardenServer.Stop()
	})

	runTask := func(block bool) {
		fakeContainer := new(wfakes.FakeContainer)
		fakeBackend.CreateReturns(fakeContainer, nil)
		fakeBackend.LookupReturns(fakeContainer, nil)
		fakeBackend.ContainersReturns([]warden.Container{fakeContainer}, nil)

		process := new(wfakes.FakeProcess)

		process.WaitStub = func() (int, error) {
			time.Sleep(time.Second)

			if block {
				select {}
			} else {
				_, io := fakeContainer.RunArgsForCall(0)

				Eventually(func() interface{} {
					_, err := io.Stdout.Write([]byte("some-output\n"))
					Ω(err).ShouldNot(HaveOccurred())

					return logConfirmations
				}).Should(Receive())
			}

			return 0, nil
		}

		fakeContainer.RunReturns(process, nil)
		fakeContainer.HandleReturns("some-handle")

		guid, err := uuid.NewV4()
		Ω(err).ShouldNot(HaveOccurred())

		_, err = executorClient.AllocateContainer(guid.String(), api.ContainerAllocationRequest{
			MemoryMB: 1024,
			DiskMB:   1024,
		})
		Ω(err).ShouldNot(HaveOccurred())

		index := 13
		_, err = executorClient.InitializeContainer(guid.String(), api.ContainerInitializationRequest{
			Log: models.LogConfig{
				Guid:       "the-app-guid",
				SourceName: "STG",
				Index:      &index,
			},
		})
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
			var fakeContainer1, fakeContainer2 *wfakes.FakeContainer

			BeforeEach(func() {
				fakeContainer1 = new(wfakes.FakeContainer)
				fakeContainer1.HandleReturns("handle-1")

				fakeContainer2 = new(wfakes.FakeContainer)
				fakeContainer2.HandleReturns("handle-2")

				fakeBackend.ContainersStub = func(ps warden.Properties) ([]warden.Container, error) {
					if reflect.DeepEqual(ps, warden.Properties{"owner": "executor-name"}) {
						return []warden.Container{fakeContainer1, fakeContainer2}, nil
					} else {
						return nil, nil
					}
				}
			})

			It("should delete those containers (and only those containers)", func() {
				runner.Start(executor_runner.Config{
					ContainerOwnerName: "executor-name",
				})

				Eventually(fakeBackend.DestroyCallCount).Should(Equal(2))
				Ω(fakeBackend.DestroyArgsForCall(0)).Should(Equal("handle-1"))
				Ω(fakeBackend.DestroyArgsForCall(1)).Should(Equal("handle-2"))
			})

			Context("when deleting the container fails", func() {
				BeforeEach(func() {
					fakeBackend.DestroyReturns(errors.New("i tried to delete the thing but i failed. sorry."))
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

		Describe("running something", func() {
			It("streams log output", func() {
				runTask(false)

				message := &logmessage.LogMessage{}
				Eventually(logMessages, 5).Should(Receive(&message))

				Ω(message.GetAppId()).Should(Equal("the-app-guid"))
				Ω(message.GetSourceName()).Should(Equal("STG"))
				Ω(message.GetMessageType()).Should(Equal(logmessage.LogMessage_OUT))
				Ω(message.GetSourceId()).Should(Equal("13"))
				Ω(string(message.GetMessage())).Should(Equal("some-output"))
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
				runner.Session.Terminate()
				eventuallyContainerShouldBeDestroyed()
			})

			It("exits successfully", func() {
				runner.Session.Terminate()
				Eventually(runner.Session, 2*drainTimeout).Should(gexec.Exit(0))
			})
		})

		Describe("when the executor receives the INT signal", func() {
			It("stops all running tasks", func() {
				runTask(true)
				runner.Session.Interrupt()
				eventuallyContainerShouldBeDestroyed()
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

						runner.Session.Signal(syscall.SIGUSR1)
						Eventually(runner.Session, time.Second).Should(gbytes.Say("executor.draining"))

						runner.Session.Signal(syscall.SIGUSR1)
						Eventually(runner.Session, 5*time.Second).Should(gbytes.Say("executor.signal.ignored"))
					})
				})

				Context("when the tasks complete before the drain timeout", func() {
					It("exits successfully", func() {
						runTask(false)

						sendDrainSignal()

						Eventually(runner.Session, drainTimeout-aBit).Should(gexec.Exit(0))
					})
				})

				Context("when the tasks do not complete before the drain timeout", func() {
					BeforeEach(func() {
						runTask(true)
					})

					It("cancels all running tasks", func() {
						sendDrainSignal()
						eventuallyContainerShouldBeDestroyed(drainTimeout + aBit)
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

func startFakeLoggregatorServer(logConfirmations chan<- struct{}) (string, <-chan *logmessage.LogMessage) {
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	Ω(err).ShouldNot(HaveOccurred())

	conn, err := net.ListenUDP("udp", addr)
	Ω(err).ShouldNot(HaveOccurred())

	logMessages := make(chan *logmessage.LogMessage, 10)

	go func() {
		defer GinkgoRecover()

		for {
			buf := make([]byte, 1024)

			rlen, _, err := conn.ReadFromUDP(buf)
			logConfirmations <- struct{}{}
			Ω(err).ShouldNot(HaveOccurred())

			message, err := logmessage.ParseEnvelope(buf[0:rlen], "the-loggregator-secret")
			Ω(err).ShouldNot(HaveOccurred())

			logMessages <- message.GetLogMessage()
		}
	}()

	return conn.LocalAddr().String(), logMessages
}
