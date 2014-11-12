package main_test

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path"
	"sync"
	"time"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/executor/cmd/executor/testrunner"
	"github.com/cloudfoundry-incubator/executor/http/client"
	garden "github.com/cloudfoundry-incubator/garden/api"
	gfakes "github.com/cloudfoundry-incubator/garden/api/fakes"
	GardenServer "github.com/cloudfoundry-incubator/garden/server"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/nu7hatch/gouuid"
	"github.com/onsi/ginkgo/config"
	"github.com/pivotal-golang/lager/lagertest"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/ginkgomon"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Commandline Options", func() {
	var (
		gardenAddr      string
		executorAddr    string
		debugAddr       string
		process         ifrit.Process
		cachePath       string
		tmpDir          string
		ownerName       string
		allowPrivileged bool

		fakeBackend *gfakes.FakeBackend
	)

	pruningInterval := 500 * time.Millisecond

	BeforeEach(func() {
		gardenPort := 9001 + GinkgoParallelNode()
		debugAddr = fmt.Sprintf("127.0.0.1:%d", 10001+GinkgoParallelNode())
		executorAddr = fmt.Sprintf("127.0.0.1:%d", 1700+GinkgoParallelNode())

		executorClient = client.New(http.DefaultClient, "http://"+executorAddr)

		gardenAddr = fmt.Sprintf("127.0.0.1:%d", gardenPort)

		fakeBackend = new(gfakes.FakeBackend)
		fakeBackend.CapacityReturns(garden.Capacity{
			MemoryInBytes: 1024 * 1024 * 1024,
			DiskInBytes:   1024 * 1024 * 1024,
			MaxContainers: 1024,
		}, nil)

		gardenServer = GardenServer.New("tcp", gardenAddr, 0, fakeBackend, lagertest.NewTestLogger("garden"))
		tmpDir = path.Join(os.TempDir(), fmt.Sprintf("executor_%d", GinkgoParallelNode()))
		cachePath = path.Join(tmpDir, "cache")
		ownerName = fmt.Sprintf("executor-on-node-%d", config.GinkgoConfig.ParallelNode)
		allowPrivileged = false
	})

	JustBeforeEach(func() {
		runner = testrunner.New(
			executorPath,
			executorAddr,
			"tcp",
			gardenAddr,
			"",
			"bogus-loggregator-secret",
			cachePath,
			tmpDir,
			debugAddr,
			ownerName,
			pruningInterval,
			allowPrivileged,
		)

		err := gardenServer.Start()
		Ω(err).ShouldNot(HaveOccurred())

		process = ginkgomon.Invoke(runner)
	})

	AfterEach(func() {
		ginkgomon.Kill(process)
		gardenServer.Stop()

		Eventually(func() error {
			conn, err := net.Dial("tcp", gardenAddr)
			if err == nil {
				conn.Close()
			}

			return err
		}).Should(HaveOccurred())
	})

	setupFakeContainer := func(guid string) *gfakes.FakeContainer {
		properties := make(garden.Properties)
		propertyMu := new(sync.RWMutex)

		fakeContainer := &gfakes.FakeContainer{
			SetPropertyStub: func(key, value string) error {
				propertyMu.Lock()
				defer propertyMu.Unlock()

				properties[key] = value
				return nil
			},

			GetPropertyStub: func(key string) (string, error) {
				propertyMu.RLock()
				defer propertyMu.RUnlock()

				return properties[key], nil
			},

			InfoStub: func() (garden.ContainerInfo, error) {
				propertyMu.RLock()
				defer propertyMu.RUnlock()

				copied := make(garden.Properties)
				for k, v := range properties {
					copied[k] = v
				}

				return garden.ContainerInfo{
					Properties: copied,
				}, nil
			},
		}

		fakeContainer.HandleReturns(guid)

		fakeBackend.CreateStub = func(spec garden.ContainerSpec) (garden.Container, error) {
			properties = spec.Properties
			return fakeContainer, nil
		}

		fakeBackend.LookupReturns(fakeContainer, nil)
		fakeBackend.ContainersReturns([]garden.Container{fakeContainer}, nil)

		return fakeContainer
	}

	Describe("allowPrivileged", func() {
		Context("when trying to run a container with a privileged run action", func() {
			var runResult executor.ContainerRunResult

			JustBeforeEach(func() {
				uuid, err := uuid.NewV4()
				Ω(err).ShouldNot(HaveOccurred())
				containerGuid := uuid.String()

				container := executor.Container{
					Guid: containerGuid,
					Actions: []models.ExecutorAction{
						{
							models.RunAction{
								Path:       "ls",
								Privileged: true,
							},
						},
					},
				}

				_, err = executorClient.AllocateContainer(container)
				Ω(err).ShouldNot(HaveOccurred())

				fakeContainer := setupFakeContainer(containerGuid)

				process := new(gfakes.FakeProcess)
				process.WaitStub = func() (int, error) {
					return 0, nil
				}

				fakeContainer.RunReturns(process, nil)

				err = executorClient.RunContainer(containerGuid)
				Ω(err).ShouldNot(HaveOccurred())

				Eventually(func() executor.State {
					container, err := executorClient.GetContainer(containerGuid)
					if err != nil {
						return executor.StateInvalid
					}

					runResult = container.RunResult
					return container.State
				}).Should(Equal(executor.StateCompleted))
			})

			Context("when allowPrivileged is set", func() {
				BeforeEach(func() {
					allowPrivileged = true
				})

				It("does not error", func() {
					Ω(runResult.Failed).Should(BeFalse())
				})
			})

			Context("when allowPrivileged is not set", func() {
				BeforeEach(func() {
					allowPrivileged = false
				})

				It("does error", func() {
					Ω(runResult.Failed).Should(BeTrue())
					Ω(runResult.FailureReason).Should(Equal("privileged-action-denied"))
				})
			})
		})
	})
})
