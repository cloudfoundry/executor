package main_test

import (
	"fmt"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/cloudfoundry-incubator/garden/backend"
	"github.com/cloudfoundry-incubator/garden/backend/fake_backend"
	"github.com/cloudfoundry/gunk/runner_support"
	"github.com/cloudfoundry/gunk/timeprovider"
	"github.com/cloudfoundry/storeadapter/storerunner/etcdstorerunner"

	GardenServer "github.com/cloudfoundry-incubator/garden/server"
	Bbs "github.com/cloudfoundry-incubator/runtime-schema/bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models/factories"
	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/config"
	. "github.com/onsi/gomega"
	"github.com/vito/cmdtest"
	. "github.com/vito/cmdtest/matchers"
)

func TestExecutorMain(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Executor Suite")
}

var _ = Describe("Main", func() {
	var (
		etcdRunner *etcdstorerunner.ETCDClusterRunner

		etcdCluster string
		wardenAddr  string

		executorPath string
		gardenServer *GardenServer.WardenServer
		bbs          *Bbs.BBS
		fakeBackend  *fake_backend.FakeBackend
	)

	drainTimeout := 5 * time.Second
	aBit := drainTimeout / 5

	BeforeEach(func() {
		var err error

		etcdPort := 5001 + config.GinkgoConfig.ParallelNode
		wardenPort := 9001 + config.GinkgoConfig.ParallelNode

		etcdRunner = etcdstorerunner.NewETCDClusterRunner(etcdPort, 1)
		etcdRunner.Start()

		etcdCluster = fmt.Sprintf("http://127.0.0.1:%d", etcdPort)

		bbs = Bbs.New(etcdRunner.Adapter(), timeprovider.NewTimeProvider())

		executorPath, err = cmdtest.Build("github.com/cloudfoundry-incubator/executor", "-race")
		Ω(err).ShouldNot(HaveOccurred())

		wardenAddr = fmt.Sprintf("127.0.0.1:%d", wardenPort)

		fakeBackend = fake_backend.New()
		gardenServer = GardenServer.New("tcp", wardenAddr, 0, fakeBackend)

		err = gardenServer.Start()
		Ω(err).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		etcdRunner.Stop()
		gardenServer.Stop()
	})

	desireRunOnce := func(duration time.Duration) {
		exitStatus := uint32(0)

		fakeContainer := fake_backend.NewFakeContainer(backend.ContainerSpec{})
		fakeContainer.StreamDelay = duration
		fakeContainer.StreamedProcessChunks = []backend.ProcessStream{
			{ExitStatus: &exitStatus},
		}

		fakeBackend.CreateResult = fakeContainer

		err := bbs.DesireRunOnce(factories.BuildRunOnceWithRunAction("the-stack", 1024, 1024, "ls"))
		Ω(err).ShouldNot(HaveOccurred())
	}

	startExecutor := func(args ...string) *cmdtest.Session {
		executorCmd := exec.Command(
			executorPath,
			append(
				[]string{
					"-wardenNetwork", "tcp",
					"-wardenAddr", wardenAddr,
					"-drainTimeout", "5s",
					"-etcdCluster", etcdCluster,
					"-stack", "the-stack",
					"-memoryMB", "10240",
					"-diskMB", "10240",
					"-heartbeatInterval", "3s",
				},
				args...,
			)...,
		)

		executorSession, err := cmdtest.StartWrapped(executorCmd, runner_support.TeeToGinkgoWriter, runner_support.TeeToGinkgoWriter)
		Ω(err).ShouldNot(HaveOccurred())

		Ω(executorSession).Should(SayWithTimeout("executor.started", 5*time.Second))

		return executorSession
	}

	stopExecutor := func(executorSession *cmdtest.Session) {
		executorSession.Cmd.Process.Kill()

		_, err := executorSession.Wait(time.Second)
		Ω(err).ShouldNot(HaveOccurred())
	}

	Describe("starting up", func() {
		Context("when there are containers that are owned by the executor", func() {
			var handleThatShouldDie string

			BeforeEach(func() {
				container, err := fakeBackend.Create(backend.ContainerSpec{Properties: backend.Properties{
					"owner": "executor-name",
				}})
				Ω(err).ShouldNot(HaveOccurred())

				handleThatShouldDie = container.Handle()

				_, err = fakeBackend.Create(backend.ContainerSpec{})
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("should delete those containers (and only those containers)", func() {
				sess := startExecutor("-containerOwnerName", "executor-name")
				defer stopExecutor(sess)

				Ω(fakeBackend.DestroyedContainers).Should(Equal([]string{handleThatShouldDie}))
			})
		})
	})

	Context("when started", func() {
		var executorSession *cmdtest.Session

		BeforeEach(func() {
			executorSession = startExecutor()
		})

		AfterEach(func() {
			stopExecutor(executorSession)
		})

		Describe("when the executor fails to maintain its presence", func() {
			It("stops all running tasks", func() {
				desireRunOnce(time.Hour)
				Eventually(fakeBackend.Containers).Should(HaveLen(1))

				// delete the executor's key (and everything else lol)
				etcdRunner.Reset()

				Eventually(fakeBackend.Containers, 7).Should(BeEmpty())
			})
		})

		Describe("when the executor receives the TERM signal", func() {
			It("stops all running tasks", func() {
				desireRunOnce(time.Hour)
				Eventually(fakeBackend.Containers).Should(HaveLen(1))

				executorSession.Cmd.Process.Signal(syscall.SIGTERM)

				Eventually(fakeBackend.Containers, 7).Should(BeEmpty())
			})

			It("exits successfully", func() {
				executorSession.Cmd.Process.Signal(syscall.SIGTERM)
				Ω(executorSession).Should(ExitWithTimeout(0, 2*drainTimeout))
			})
		})

		Describe("when the executor receives the INT signal", func() {
			It("stops all running tasks", func() {
				desireRunOnce(time.Hour)
				Eventually(fakeBackend.Containers).Should(HaveLen(1))

				executorSession.Cmd.Process.Signal(syscall.SIGINT)

				Eventually(fakeBackend.Containers, 7).Should(BeEmpty())
			})

			It("exits successfully", func() {
				executorSession.Cmd.Process.Signal(syscall.SIGINT)
				Ω(executorSession).Should(ExitWithTimeout(0, 2*drainTimeout))
			})
		})

		Describe("when the executor receives the USR1 signal", func() {
			sendDrainSignal := func() {
				executorSession.Cmd.Process.Signal(syscall.SIGUSR1)
				Ω(executorSession).Should(SayWithTimeout("executor.draining", time.Second))
			}

			Context("when there are tasks running", func() {
				It("stops accepting new tasks", func() {
					sendDrainSignal()

					desireRunOnce(time.Hour)
					Consistently(bbs.GetAllPendingRunOnces, 2).Should(HaveLen(1))
				})

				Context("and USR1 is received again", func() {
					It("does not die", func() {
						desireRunOnce(time.Hour)
						Eventually(bbs.GetAllStartingRunOnces).Should(HaveLen(1))

						executorSession.Cmd.Process.Signal(syscall.SIGUSR1)
						Ω(executorSession).Should(SayWithTimeout("executor.draining", time.Second))

						executorSession.Cmd.Process.Signal(syscall.SIGUSR1)
						Ω(executorSession).Should(SayWithTimeout("executor.signal.ignored", 5*time.Second))
					})
				})

				Context("when the tasks complete before the drain timeout", func() {
					It("exits successfully", func() {
						desireRunOnce(1 * time.Second)
						Eventually(bbs.GetAllStartingRunOnces).Should(HaveLen(1))

						sendDrainSignal()

						Ω(executorSession).Should(ExitWithTimeout(0, drainTimeout-aBit))
					})
				})

				Context("when the tasks do not complete before the drain timeout", func() {
					BeforeEach(func() {
						desireRunOnce(time.Hour)
						Eventually(bbs.GetAllStartingRunOnces).Should(HaveLen(1))
					})

					It("cancels all running tasks", func() {
						Ω(fakeBackend.Containers()).Should(HaveLen(1))
						sendDrainSignal()
						Eventually(fakeBackend.Containers, (drainTimeout + aBit).Seconds()).Should(BeEmpty())
					})

					It("exits successfully", func() {
						sendDrainSignal()
						Ω(executorSession).Should(ExitWithTimeout(0, 2*drainTimeout))
					})
				})
			})

			Context("when there are no tasks running", func() {
				It("exits successfully", func() {
					sendDrainSignal()
					Ω(executorSession).Should(ExitWithTimeout(0, 1*time.Second))
				})
			})
		})
	})
})
