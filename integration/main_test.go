package integration_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"syscall"
	"testing"
	"time"

	GardenServer "github.com/cloudfoundry-incubator/garden/server"
	"github.com/cloudfoundry-incubator/garden/warden"
	"github.com/cloudfoundry-incubator/garden/warden/fake_backend"
	Bbs "github.com/cloudfoundry-incubator/runtime-schema/bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry-incubator/runtime-schema/models/executor_api"
	"github.com/cloudfoundry/gunk/timeprovider"
	"github.com/cloudfoundry/storeadapter/storerunner/etcdstorerunner"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/gexec"
	"github.com/tedsuo/router"

	"github.com/cloudfoundry-incubator/executor/integration/executor_runner"
)

func TestExecutorMain(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Integration Suite")
}

var executorPath string
var etcdRunner *etcdstorerunner.ETCDClusterRunner
var gardenServer *GardenServer.WardenServer
var runner *executor_runner.ExecutorRunner
var reqGen *router.RequestGenerator

var _ = BeforeSuite(func() {
	var err error
	executorPath, err = gexec.Build("github.com/cloudfoundry-incubator/executor", "-race")
	Ω(err).ShouldNot(HaveOccurred())
})

var _ = AfterSuite(func() {
	gexec.CleanupBuildArtifacts()
	if etcdRunner != nil {
		etcdRunner.Stop()
	}

	if gardenServer != nil {
		gardenServer.Stop()
	}

	if runner != nil {
		runner.KillWithFire()
	}
})

var _ = Describe("Main", func() {
	var (
		etcdCluster []string
		wardenAddr  string

		bbs         *Bbs.BBS
		fakeBackend *fake_backend.FakeBackend
	)

	drainTimeout := 5 * time.Second
	aBit := drainTimeout / 5

	BeforeEach(func() {
		var err error

		etcdPort := 5001 + GinkgoParallelNode()
		wardenPort := 9001 + GinkgoParallelNode()
		executorAddr := fmt.Sprintf("127.0.0.1:%d", 1700+GinkgoParallelNode())

		reqGen = router.NewRequestGenerator("http://"+executorAddr, executor_api.Routes)

		etcdRunner = etcdstorerunner.NewETCDClusterRunner(etcdPort, 1)
		etcdRunner.Start()

		etcdCluster = []string{fmt.Sprintf("http://127.0.0.1:%d", etcdPort)}

		bbs = Bbs.New(etcdRunner.Adapter(), timeprovider.NewTimeProvider())

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

	runTask := func(duration time.Duration) {
		exitStatus := uint32(0)

		fakeContainer := fake_backend.NewFakeContainer(warden.ContainerSpec{
			Properties: warden.Properties{
				"owner": runner.Config.ContainerOwnerName,
			},
		})
		fakeContainer.StreamDelay = duration
		fakeContainer.StreamedProcessChunks = []warden.ProcessStream{
			{ExitStatus: &exitStatus},
		}

		fakeBackend.CreateResult = fakeContainer

		req := executor_api.ContainerAllocationRequest{
			MemoryMB: 1024,
			DiskMB:   1024,
		}
		container := executor_api.Container{}
		PerformRequest(reqGen, executor_api.AllocateContainer, nil, req, &container)

		params := router.Params{"guid": container.Guid}
		PerformRequest(reqGen, executor_api.InitializeContainer, params, nil, nil)

		actions := executor_api.ContainerRunRequest{
			Actions: []models.ExecutorAction{
				{Action: models.RunAction{Script: "ls"}},
			},
		}
		PerformRequest(reqGen, executor_api.RunActions, params, actions, nil)
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
					fakeBackend.DestroyError = errors.New("i tried to delete the thing but i failed. sorry.")
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
				HeartbeatInterval: 3 * time.Second,
				Stack:             "the-stack",
				DrainTimeout:      drainTimeout,
			})
		})

		Describe("when the executor fails to maintain its presence", func() {
			It("stops all running tasks", func() {
				runTask(time.Hour)
				Eventually(pollContainers(fakeBackend), 10).Should(HaveLen(1))

				// delete the executor's key (and everything else lol)
				etcdRunner.Reset()

				Eventually(pollContainers(fakeBackend), 5.0).Should(BeEmpty())
			})
		})

		Describe("when the executor receives the TERM signal", func() {
			It("stops all running tasks", func() {
				runTask(time.Hour)
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
				runTask(time.Hour)
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

					body := MarshalledPayload(executor_api.ContainerAllocationRequest{})
					req, err := reqGen.RequestForHandler(executor_api.AllocateContainer, nil, body)
					Ω(err).ShouldNot(HaveOccurred())

					_, err = http.DefaultClient.Do(req)
					Ω(err).Should(HaveOccurred())
				})

				Context("and USR1 is received again", func() {
					It("does not die", func() {
						runTask(time.Hour)
						Eventually(pollContainers(fakeBackend)).Should(HaveLen(1))

						runner.Session.Signal(syscall.SIGUSR1)
						Eventually(runner.Session, time.Second).Should(gbytes.Say("executor.draining"))

						runner.Session.Signal(syscall.SIGUSR1)
						Eventually(runner.Session, 5*time.Second).Should(gbytes.Say("executor.signal.ignored"))
					})
				})

				Context("when the tasks complete before the drain timeout", func() {
					It("exits successfully", func() {
						runTask(1 * time.Second)
						Eventually(pollContainers(fakeBackend)).Should(HaveLen(1))

						sendDrainSignal()

						Eventually(runner.Session, drainTimeout-aBit).Should(gexec.Exit(0))
					})
				})

				Context("when the tasks do not complete before the drain timeout", func() {
					BeforeEach(func() {
						runTask(time.Hour)
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

func PerformRequest(reqGen *router.RequestGenerator, handler string, params router.Params, reqBody interface{}, resBody interface{}) {
	body := MarshalledPayload(reqBody)

	req, err := reqGen.RequestForHandler(handler, params, body)
	Ω(err).ShouldNot(HaveOccurred())

	client := &http.Client{
		Transport: &http.Transport{},
	}

	resp, err := client.Do(req)
	Ω(err).ShouldNot(HaveOccurred())

	Ω(resp.StatusCode).Should(BeNumerically("<", 400))

	json.NewDecoder(resp.Body).Decode(resBody)
}

func MarshalledPayload(payload interface{}) io.Reader {
	reqBody, err := json.Marshal(payload)
	Ω(err).ShouldNot(HaveOccurred())

	return bytes.NewBuffer(reqBody)
}
