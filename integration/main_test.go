package integration_test

import (
	"errors"
	"fmt"
	"syscall"
	"testing"
	"time"

	"github.com/cloudfoundry-incubator/garden/backend"
	"github.com/cloudfoundry-incubator/garden/backend/fake_backend"
	GardenServer "github.com/cloudfoundry-incubator/garden/server"
	Bbs "github.com/cloudfoundry-incubator/runtime-schema/bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models/factories"
	"github.com/cloudfoundry/gunk/timeprovider"
	"github.com/cloudfoundry/storeadapter/storerunner/etcdstorerunner"
	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/config"
	. "github.com/onsi/gomega"
	"github.com/vito/cmdtest"
	. "github.com/vito/cmdtest/matchers"

	"github.com/cloudfoundry-incubator/executor/integration/executor_runner"
)

func TestExecutorMain(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Executor Suite")
}

var _ = Describe("Main", func() {
	var (
		etcdRunner *etcdstorerunner.ETCDClusterRunner

		etcdCluster []string
		wardenAddr  string

		executorPath string
		runner       *executor_runner.ExecutorRunner

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

		etcdCluster = []string{fmt.Sprintf("http://127.0.0.1:%d", etcdPort)}

		bbs = Bbs.New(etcdRunner.Adapter(), timeprovider.NewTimeProvider())

		executorPath, err = cmdtest.Build("github.com/cloudfoundry-incubator/executor", "-race")
		Ω(err).ShouldNot(HaveOccurred())

		wardenAddr = fmt.Sprintf("127.0.0.1:%d", wardenPort)

		fakeBackend = fake_backend.New()
		gardenServer = GardenServer.New("tcp", wardenAddr, 0, fakeBackend)

		err = gardenServer.Start()
		Ω(err).ShouldNot(HaveOccurred())

		runner = executor_runner.New(
			executorPath,
			"tcp",
			wardenAddr,
			etcdCluster,
			"",
			"",
		)
	})

	AfterEach(func() {
		runner.KillWithFire()
		etcdRunner.Stop()
		gardenServer.Stop()
	})

	desireTask := func(duration time.Duration) {
		exitStatus := uint32(0)

		fakeContainer := fake_backend.NewFakeContainer(backend.ContainerSpec{})
		fakeContainer.StreamDelay = duration
		fakeContainer.StreamedProcessChunks = []backend.ProcessStream{
			{ExitStatus: &exitStatus},
		}

		fakeBackend.CreateResult = fakeContainer

		err := bbs.DesireTask(factories.BuildTaskWithRunAction("the-stack", 1024, 1024, "ls"))
		Ω(err).ShouldNot(HaveOccurred())
	}

	Describe("starting up", func() {
		Context("when there are containers that are owned by the executor", func() {
			var handleThatShouldDie string

			BeforeEach(func() {
				container, err := fakeBackend.Create(backend.ContainerSpec{
					Handle: "container-that-should-die",
					Properties: backend.Properties{
						"owner": "executor-name",
					},
				})
				Ω(err).ShouldNot(HaveOccurred())

				handleThatShouldDie = container.Handle()

				_, err = fakeBackend.Create(backend.ContainerSpec{
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
					fakeBackend.DestroyError = errors.New("i tried to delete the thing but i failed. sorry.")
				})

				It("should exit sadly", func() {
					runner.StartWithoutCheck(executor_runner.Config{
						ContainerOwnerName: "executor-name",
					})

					Ω(runner.Session).Should(ExitWithTimeout(1, 1*time.Second))
				})
			})
		})
	})

	Context("when started", func() {
		BeforeEach(func() {
			runner.Start(executor_runner.Config{
				HeartbeatInterval: 3 * time.Second,
				Stack:             "the-stack",
				DrainTimeout:      1 * time.Second,
			})
		})

		Describe("when the executor fails to maintain its presence", func() {
			It("stops all running tasks", func() {
				desireTask(time.Hour)
				Eventually(fakeBackend.Containers).Should(HaveLen(1))

				// delete the executor's key (and everything else lol)
				etcdRunner.Reset()

				Eventually(fakeBackend.Containers, 7).Should(BeEmpty())
			})
		})

		Describe("when the executor receives the TERM signal", func() {
			It("stops all running tasks", func() {
				desireTask(time.Hour)
				Eventually(fakeBackend.Containers).Should(HaveLen(1))

				runner.Session.Cmd.Process.Signal(syscall.SIGTERM)

				Eventually(fakeBackend.Containers, 7).Should(BeEmpty())
			})

			It("exits successfully", func() {
				runner.Session.Cmd.Process.Signal(syscall.SIGTERM)
				Ω(runner.Session).Should(ExitWithTimeout(0, 2*drainTimeout))
			})
		})

		Describe("when the executor receives the INT signal", func() {
			It("stops all running tasks", func() {
				desireTask(time.Hour)
				Eventually(fakeBackend.Containers).Should(HaveLen(1))

				runner.Session.Cmd.Process.Signal(syscall.SIGINT)

				Eventually(fakeBackend.Containers, 7).Should(BeEmpty())
			})

			It("exits successfully", func() {
				runner.Session.Cmd.Process.Signal(syscall.SIGINT)
				Ω(runner.Session).Should(ExitWithTimeout(0, 2*drainTimeout))
			})
		})

		Describe("when the executor receives the USR1 signal", func() {
			sendDrainSignal := func() {
				runner.Session.Cmd.Process.Signal(syscall.SIGUSR1)
				Ω(runner.Session).Should(SayWithTimeout("executor.draining", time.Second))
			}

			Context("when there are tasks running", func() {
				It("stops accepting new tasks", func() {
					sendDrainSignal()

					desireTask(time.Hour)
					Consistently(bbs.GetAllPendingTasks, 2).Should(HaveLen(1))
				})

				Context("and USR1 is received again", func() {
					It("does not die", func() {
						desireTask(time.Hour)
						Eventually(bbs.GetAllStartingTasks).Should(HaveLen(1))

						runner.Session.Cmd.Process.Signal(syscall.SIGUSR1)
						Ω(runner.Session).Should(SayWithTimeout("executor.draining", time.Second))

						runner.Session.Cmd.Process.Signal(syscall.SIGUSR1)
						Ω(runner.Session).Should(SayWithTimeout("executor.signal.ignored", 5*time.Second))
					})
				})

				Context("when the tasks complete before the drain timeout", func() {
					It("exits successfully", func() {
						desireTask(1 * time.Second)
						Eventually(bbs.GetAllStartingTasks).Should(HaveLen(1))

						sendDrainSignal()

						Ω(runner.Session).Should(ExitWithTimeout(0, drainTimeout-aBit))
					})
				})

				Context("when the tasks do not complete before the drain timeout", func() {
					BeforeEach(func() {
						desireTask(time.Hour)
						Eventually(bbs.GetAllStartingTasks).Should(HaveLen(1))
					})

					It("cancels all running tasks", func() {
						Ω(fakeBackend.Containers()).Should(HaveLen(1))
						sendDrainSignal()
						Eventually(fakeBackend.Containers, (drainTimeout + aBit).Seconds()).Should(BeEmpty())
					})

					It("exits successfully", func() {
						sendDrainSignal()
						Ω(runner.Session).Should(ExitWithTimeout(0, 2*drainTimeout))
					})
				})
			})

			Context("when there are no tasks running", func() {
				It("exits successfully", func() {
					sendDrainSignal()
					Ω(runner.Session).Should(ExitWithTimeout(0, 1*time.Second))
				})
			})
		})
	})
})
