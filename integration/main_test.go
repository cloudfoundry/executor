package integration_test

import (
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/nu7hatch/gouuid"
	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/config"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/gexec"
	"github.com/onsi/gomega/ghttp"

	"github.com/cloudfoundry-incubator/executor/api"
	"github.com/cloudfoundry-incubator/executor/client"
	GardenServer "github.com/cloudfoundry-incubator/garden/server"
	"github.com/cloudfoundry-incubator/garden/warden"
	wfakes "github.com/cloudfoundry-incubator/garden/warden/fakes"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/loggregatorlib/logmessage"
	"github.com/pivotal-golang/lager/lagertest"

	"github.com/cloudfoundry-incubator/executor/integration/executor_runner"
)

func TestExecutorMain(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Integration Suite")
}

var executorPath string
var gardenServer *GardenServer.WardenServer
var runner *executor_runner.ExecutorRunner
var executorClient api.Client

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
		debugAddr  string

		fakeBackend *wfakes.FakeBackend

		logMessages      <-chan *logmessage.LogMessage
		logConfirmations <-chan struct{}
	)

	pruningInterval := 500 * time.Millisecond

	BeforeEach(func() {
		//gardenServer.Stop calls listener.Close()
		//apparently listener.Close() returns before the port is *actually* closed...?
		wardenPort := 9001 + CurrentGinkgoTestDescription().LineNumber
		debugAddr = fmt.Sprintf("127.0.0.1:%d", 10001+GinkgoParallelNode())
		executorAddr := fmt.Sprintf("127.0.0.1:%d", 1700+GinkgoParallelNode())

		executorClient = client.New(http.DefaultClient, "http://"+executorAddr)

		wardenAddr = fmt.Sprintf("127.0.0.1:%d", wardenPort)

		fakeBackend = new(wfakes.FakeBackend)
		fakeBackend.CapacityReturns(warden.Capacity{
			MemoryInBytes: 1024 * 1024 * 1024,
			DiskInBytes:   1024 * 1024 * 1024,
			MaxContainers: 1024,
		}, nil)

		gardenServer = GardenServer.New("tcp", wardenAddr, 0, fakeBackend, lagertest.NewTestLogger("garden"))

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

	allocNewContainer := func(request api.ContainerAllocationRequest) (guid string, fakeContainer *wfakes.FakeContainer) {
		fakeContainer = new(wfakes.FakeContainer)
		fakeBackend.CreateReturns(fakeContainer, nil)
		fakeBackend.LookupReturns(fakeContainer, nil)
		fakeBackend.ContainersReturns([]warden.Container{fakeContainer}, nil)
		fakeContainer.HandleReturns("some-handle")

		id, err := uuid.NewV4()
		Ω(err).ShouldNot(HaveOccurred())

		guid = id.String()

		_, err = executorClient.AllocateContainer(guid, request)
		Ω(err).ShouldNot(HaveOccurred())

		return guid, fakeContainer
	}

	initNewContainer := func() (guid string, fakeContainer *wfakes.FakeContainer) {
		guid, fakeContainer = allocNewContainer(api.ContainerAllocationRequest{
			MemoryMB: 1024,
			DiskMB:   1024,
		})

		index := 13
		_, err := executorClient.InitializeContainer(guid, api.ContainerInitializationRequest{
			Log: api.LogConfig{
				Guid:       "the-app-guid",
				SourceName: "STG",
				Index:      &index,
			},
		})
		Ω(err).ShouldNot(HaveOccurred())

		return guid, fakeContainer
	}

	runTask := func(block bool) {
		guid, fakeContainer := initNewContainer()

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

		err := executorClient.Run(guid, api.ContainerRunRequest{
			Actions: []models.ExecutorAction{
				{Action: models.RunAction{Path: "ls"}},
			},
		})
		Ω(err).ShouldNot(HaveOccurred())
	}

	Describe("starting up", func() {
		JustBeforeEach(func() {
			err := gardenServer.Start()
			Ω(err).ShouldNot(HaveOccurred())

			runner.StartWithoutCheck(executor_runner.Config{
				ContainerOwnerName: "executor-name",
			})
		})

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

			It("deletes those containers (and only those containers)", func() {
				Eventually(fakeBackend.DestroyCallCount).Should(Equal(2))
				Ω(fakeBackend.DestroyArgsForCall(0)).Should(Equal("handle-1"))
				Ω(fakeBackend.DestroyArgsForCall(1)).Should(Equal("handle-2"))
			})

			It("reports itself as started via logs", func() {
				Eventually(runner.Session).Should(gbytes.Say("executor.started"))
			})

			Context("when deleting the container fails", func() {
				BeforeEach(func() {
					fakeBackend.DestroyReturns(errors.New("i tried to delete the thing but i failed. sorry."))
				})

				It("should exit sadly", func() {
					Eventually(runner.Session).Should(gexec.Exit(2))
				})
			})
		})
	})

	Describe("when started", func() {
		BeforeEach(func() {
			err := gardenServer.Start()
			Ω(err).ShouldNot(HaveOccurred())

			runner.Start(executor_runner.Config{
				RegistryPruningInterval: pruningInterval,
				DebugAddr:               debugAddr,
				ContainerInodeLimit:     245000,
			})
		})

		Describe("pinging the server", func() {
			var pingErr error

			JustBeforeEach(func() {
				pingErr = executorClient.Ping()
			})

			Context("when Warden responds to ping", func() {
				BeforeEach(func() {
					fakeBackend.PingReturns(nil)
				})

				It("not return an error", func() {
					Ω(pingErr).ShouldNot(HaveOccurred())
				})
			})

			Context("when Warden returns an error", func() {
				BeforeEach(func() {
					fakeBackend.PingReturns(errors.New("oh no!"))
				})

				It("should return an error", func() {
					Ω(pingErr.Error()).Should(ContainSubstring("status: 502"))
				})
			})
		})

		Describe("getting the total resources", func() {
			var resources api.ExecutorResources
			var resourceErr error
			JustBeforeEach(func() {
				resources, resourceErr = executorClient.TotalResources()
			})

			It("not return an error", func() {
				Ω(resourceErr).ShouldNot(HaveOccurred())
			})

			It("returns the preset capacity", func() {
				expectedResources := api.ExecutorResources{
					MemoryMB:   1024,
					DiskMB:     1024,
					Containers: 1024,
				}
				Ω(resources).Should(Equal(expectedResources))
			})
		})

		Describe("allocating the container", func() {
			var guid string
			var allocatedContainer api.Container
			var allocErr error

			BeforeEach(func() {
				id, err := uuid.NewV4()
				Ω(err).ShouldNot(HaveOccurred())
				guid = id.String()
			})

			JustBeforeEach(func() {
				allocatedContainer, allocErr = executorClient.AllocateContainer(guid, api.ContainerAllocationRequest{
					MemoryMB: 256,
					DiskMB:   256,
				})
			})

			Context("when there are containers available", func() {
				It("does not return an error", func() {
					Ω(allocErr).ShouldNot(HaveOccurred())
				})

				It("returns a container", func() {
					Ω(allocatedContainer.Guid).Should(Equal(guid))
					Ω(allocatedContainer.MemoryMB).Should(Equal(256))
					Ω(allocatedContainer.DiskMB).Should(Equal(256))
					Ω(allocatedContainer.State).Should(Equal("reserved"))
					Ω(allocatedContainer.AllocatedAt).Should(BeNumerically("~", time.Now().UnixNano(), time.Second))
				})

				Context("and we get the container", func() {
					var container api.Container
					var getErr error
					JustBeforeEach(func() {
						container, getErr = executorClient.GetContainer(guid)
					})

					It("does not return an error", func() {
						Ω(getErr).ShouldNot(HaveOccurred())
					})

					It("retains the reserved containers", func() {
						Ω(container.Guid).Should(Equal(guid))
						Ω(container.MemoryMB).Should(Equal(256))
						Ω(container.DiskMB).Should(Equal(256))
						Ω(container.State).Should(Equal("reserved"))
						Ω(container.AllocatedAt).Should(BeNumerically("~", time.Now().UnixNano(), time.Second))
					})

					It("reduces the capacity by the amount reserved", func() {
						Ω(executorClient.RemainingResources()).Should(Equal(api.ExecutorResources{
							MemoryMB:   768,
							DiskMB:     768,
							Containers: 1023,
						}))
					})
				})
			})

			Context("when the container cannot be reserved because the guid is already taken", func() {
				BeforeEach(func() {
					_, err := executorClient.AllocateContainer(guid, api.ContainerAllocationRequest{
						MemoryMB: 1,
						DiskMB:   1,
					})
					Ω(err).ShouldNot(HaveOccurred())
				})

				It("returns an error", func() {
					Ω(allocErr).Should(Equal(api.ErrContainerGuidNotAvailable))
				})
			})

			Context("when the container cannot be reserved because there is no room", func() {
				BeforeEach(func() {
					_, err := executorClient.AllocateContainer("another-container-guid", api.ContainerAllocationRequest{
						MemoryMB: 1024,
						DiskMB:   1024,
					})
					Ω(err).ShouldNot(HaveOccurred())
				})

				It("returns an error", func() {
					Ω(allocErr).Should(Equal(api.ErrInsufficientResourcesAvailable))
				})
			})
		})

		Describe("initializing the container", func() {
			var initializeContainerRequest api.ContainerInitializationRequest
			var err error
			var initializedContainer api.Container
			var container *wfakes.FakeContainer
			var guid string

			BeforeEach(func() {
				guid, container = allocNewContainer(api.ContainerAllocationRequest{
					MemoryMB: 1024,
					DiskMB:   1024,
				})

				initializeContainerRequest = api.ContainerInitializationRequest{
					CpuPercent: 50.0,
				}
			})

			JustBeforeEach(func() {
				initializedContainer, err = executorClient.InitializeContainer(guid, initializeContainerRequest)
			})

			Context("when the requested CPU percent is > 100", func() {
				BeforeEach(func() {
					initializeContainerRequest = api.ContainerInitializationRequest{
						CpuPercent: 101.0,
					}
				})

				It("returns an error", func() {
					Ω(err).Should(HaveOccurred())
					Ω(err).Should(Equal(api.ErrLimitsInvalid))
				})
			})

			Context("when the requested CPU percent is < 0", func() {
				BeforeEach(func() {
					initializeContainerRequest = api.ContainerInitializationRequest{
						CpuPercent: -14.0,
					}
				})

				It("returns an error", func() {
					Ω(err).Should(HaveOccurred())
					Ω(err).Should(Equal(api.ErrLimitsInvalid))
				})
			})

			Context("when the container specifies a docker root_fs", func() {
				var expectedRootFS = "docker:///docker.com/my-image"

				BeforeEach(func() {
					initializeContainerRequest = api.ContainerInitializationRequest{
						RootFSPath: expectedRootFS,
					}
					fakeBackend.CreateReturns(container, nil)
				})

				It("doesn't return an error", func() {
					Ω(err).ShouldNot(HaveOccurred())
				})

				It("creates it with the configured rootfs", func() {
					Ω(fakeBackend.CreateCallCount()).Should(Equal(1))
					created := fakeBackend.CreateArgsForCall(0)
					Ω(created.RootFSPath).Should(Equal(expectedRootFS))
				})

				It("records the rootfs on the container", func() {
					c, err := executorClient.GetContainer(guid)
					Ω(err).ShouldNot(HaveOccurred())
					Ω(c.RootFSPath).Should(Equal(expectedRootFS))
				})
			})

			Context("when the container can be created", func() {
				BeforeEach(func() {
					fakeBackend.CreateReturns(container, nil)
				})

				It("doesn't return an error", func() {
					Ω(err).ShouldNot(HaveOccurred())
				})

				It("creates it with the configured owner", func() {
					created := fakeBackend.CreateArgsForCall(0)
					ownerName := fmt.Sprintf("executor-on-node-%d", config.GinkgoConfig.ParallelNode)
					Ω(created.Properties["owner"]).Should(Equal(ownerName))
				})

				It("applies the memory, disk, cpu and inode limits", func() {
					limitedMemory := container.LimitMemoryArgsForCall(0)
					Ω(limitedMemory.LimitInBytes).Should(Equal(uint64(1024 * 1024 * 1024)))

					limitedDisk := container.LimitDiskArgsForCall(0)
					Ω(limitedDisk.ByteHard).Should(Equal(uint64(1024 * 1024 * 1024)))
					Ω(limitedDisk.InodeHard).Should(Equal(uint64(245000)))

					limitedCPU := container.LimitCPUArgsForCall(0)
					Ω(limitedCPU.LimitInShares).Should(Equal(uint64(512)))
				})

				Context("when the container has no memory limits requested", func() {
					BeforeEach(func() {
						guid, container = allocNewContainer(api.ContainerAllocationRequest{
							MemoryMB: 0,
							DiskMB:   0,
						})
					})

					It("does not enforce a zero-value memory limit", func() {
						Ω(container.LimitMemoryCallCount()).Should(Equal(0))
					})

					It("enforces the inode limit, and a zero-value byte limit (which means unlimited)", func() {
						limitedDisk := container.LimitDiskArgsForCall(0)
						Ω(limitedDisk.ByteHard).Should(BeZero())
						Ω(limitedDisk.InodeHard).Should(Equal(uint64(245000)))
					})
				})

				Context("when the container has no CPU limits requested", func() {
					BeforeEach(func() {
						initializeContainerRequest.CpuPercent = 0
					})

					It("does not enforce a zero-value limit", func() {
						Ω(container.LimitCPUCallCount()).Should(Equal(0))
					})
				})

				Context("when ports are exposed", func() {
					BeforeEach(func() {
						initializeContainerRequest = api.ContainerInitializationRequest{
							CpuPercent: 0.5,
							Ports: []api.PortMapping{
								{ContainerPort: 8080, HostPort: 0},
								{ContainerPort: 8081, HostPort: 1234},
							},
						}
					})

					It("exposes the configured ports", func() {
						netInH, netInC := container.NetInArgsForCall(0)
						Ω(netInH).Should(Equal(uint32(0)))
						Ω(netInC).Should(Equal(uint32(8080)))

						netInH, netInC = container.NetInArgsForCall(1)
						Ω(netInH).Should(Equal(uint32(1234)))
						Ω(netInC).Should(Equal(uint32(8081)))
					})

					Context("when net-in succeeds", func() {
						BeforeEach(func() {
							calls := uint32(0)
							container.NetInStub = func(uint32, uint32) (uint32, uint32, error) {
								calls++
								return 1234 * calls, 4567 * calls, nil
							}
						})

						It("returns the container with mapped ports", func() {
							Ω(initializedContainer.Ports).Should(Equal([]api.PortMapping{
								{HostPort: 1234, ContainerPort: 4567},
								{HostPort: 2468, ContainerPort: 9134},
							}))
						})
					})

					Context("when mapping the ports fails", func() {
						BeforeEach(func() {
							container.NetInReturns(0, 0, errors.New("oh no!"))
						})

						It("returns an error", func() {
							Ω(err).Should(HaveOccurred())
							Ω(err.Error()).Should(ContainSubstring("status: 500"))
						})
					})
				})

				Context("when a zero-value CPU percentage is specified", func() {
					BeforeEach(func() {
						initializeContainerRequest = api.ContainerInitializationRequest{
							CpuPercent: 0,
						}
					})

					It("does not apply it", func() {
						Ω(container.LimitCPUCallCount()).Should(BeZero())
					})
				})

				Context("when limiting memory fails", func() {
					BeforeEach(func() {
						container.LimitMemoryReturns(errors.New("oh no!"))
					})

					It("returns an error", func() {
						Ω(err).Should(HaveOccurred())
						Ω(err.Error()).Should(ContainSubstring("status: 500"))
					})
				})

				Context("when limiting disk fails", func() {
					BeforeEach(func() {
						container.LimitDiskReturns(errors.New("oh no!"))
					})

					It("returns an error", func() {
						Ω(err).Should(HaveOccurred())
						Ω(err.Error()).Should(ContainSubstring("status: 500"))
					})
				})

				Context("when limiting CPU fails", func() {
					BeforeEach(func() {
						container.LimitCPUReturns(errors.New("oh no!"))
					})

					It("returns an error", func() {
						Ω(err).Should(HaveOccurred())
						Ω(err.Error()).Should(ContainSubstring("status: 500"))
					})
				})
			})

			Context("when for some reason the container fails to create", func() {
				BeforeEach(func() {
					fakeBackend.CreateReturns(nil, errors.New("oh no!"))
				})

				It("returns an error", func() {
					Ω(err).Should(HaveOccurred())
					Ω(err.Error()).Should(ContainSubstring("status: 500"))
				})
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

			It("fails when the actions are invalid", func() {
				guid, _ := initNewContainer()
				err := executorClient.Run(
					guid,
					api.ContainerRunRequest{
						Actions: []models.ExecutorAction{
							{
								models.MonitorAction{
									HealthyHook: models.HealthRequest{
										URL: "some/bogus/url",
									},
									Action: models.ExecutorAction{
										models.RunAction{
											Path: "ls",
											Args: []string{"-al"},
										},
									},
								},
							},
						},
					},
				)
				Ω(err).Should(HaveOccurred())
			})

			It("propagates global environment variables to each run action", func() {
				guid, fakeContainer := initNewContainer()

				process := new(wfakes.FakeProcess)
				fakeContainer.RunReturns(process, nil)

				err := executorClient.Run(
					guid,
					api.ContainerRunRequest{
						Env: []api.EnvironmentVariable{
							{Name: "ENV1", Value: "val1"},
							{Name: "ENV2", Value: "val2"},
						},
						Actions: []models.ExecutorAction{
							{
								models.RunAction{
									Path: "ls",
									Env: []models.EnvironmentVariable{
										{Name: "RUN_ENV1", Value: "run_val1"},
										{Name: "RUN_ENV2", Value: "run_val2"},
									},
								},
							},
						},
					},
				)
				Ω(err).ShouldNot(HaveOccurred())

				Eventually(fakeContainer.RunCallCount, 10).Should(Equal(1))

				spec, _ := fakeContainer.RunArgsForCall(0)
				Ω(spec.Path).Should(Equal("ls"))
				Ω(spec.Env).Should(Equal([]string{
					"ENV1=val1",
					"ENV2=val2",
					"RUN_ENV1=run_val1",
					"RUN_ENV2=run_val2",
				}))
			})

			Context("when there is a completeURL and metadata", func() {
				var callbackHandler *ghttp.Server
				var containerGuid string
				var fakeContainer *wfakes.FakeContainer

				BeforeEach(func() {
					callbackHandler = ghttp.NewServer()

					containerGuid, fakeContainer = initNewContainer()

					process := new(wfakes.FakeProcess)
					fakeContainer.RunReturns(process, nil)

					err := executorClient.Run(
						containerGuid,
						api.ContainerRunRequest{
							Actions: []models.ExecutorAction{
								{
									models.RunAction{
										Path: "ls",
										Args: []string{"-al"},
									},
								},
							},
							CompleteURL: callbackHandler.URL() + "/result",
						},
					)

					Ω(err).ShouldNot(HaveOccurred())
				})

				AfterEach(func() {
					callbackHandler.Close()
				})

				Context("and the completeURL succeeds", func() {
					BeforeEach(func() {
						callbackHandler.AppendHandlers(
							ghttp.CombineHandlers(
								ghttp.VerifyRequest("PUT", "/result"),
								ghttp.VerifyJSONRepresenting(api.ContainerRunResult{
									Guid:          containerGuid,
									Failed:        false,
									FailureReason: "",
									Result:        "",
								}),
							),
						)
					})

					It("invokes the callback with failed false", func() {
						Eventually(callbackHandler.ReceivedRequests).Should(HaveLen(1))
					})

					It("marks the container as completed", func() {
						Eventually(callbackHandler.ReceivedRequests).Should(HaveLen(1))
						container, err := executorClient.GetContainer(containerGuid)
						Ω(err).ShouldNot(HaveOccurred())
						Ω(container.State).Should(Equal(api.StateCompleted))
					})

					It("does not free the container's reserved resources", func() {
						Consistently(executorClient.RemainingResources).Should(Equal(api.ExecutorResources{
							MemoryMB:   0,
							DiskMB:     0,
							Containers: 1023,
						}))
					})
				})

				Context("and the completeURL fails", func() {
					BeforeEach(func() {
						callbackHandler.AppendHandlers(
							http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
								callbackHandler.HTTPTestServer.CloseClientConnections()
							}),
							ghttp.RespondWith(http.StatusInternalServerError, ""),
							ghttp.RespondWith(http.StatusOK, ""),
						)
					})

					It("invokes the callback repeatedly", func() {
						Eventually(callbackHandler.ReceivedRequests, 5).Should(HaveLen(3))
					})
				})
			})

			Context("when the actions fail", func() {
				var disaster = errors.New("because i said so")
				var fakeContainer *wfakes.FakeContainer
				var containerGuid string

				BeforeEach(func() {
					containerGuid, fakeContainer = initNewContainer()
					process := new(wfakes.FakeProcess)
					fakeContainer.RunReturns(process, nil)
					process.WaitReturns(1, disaster)
				})

				Context("and there is a completeURL", func() {
					var callbackHandler *ghttp.Server

					BeforeEach(func() {
						callbackHandler = ghttp.NewServer()

						callbackHandler.AppendHandlers(
							ghttp.CombineHandlers(
								ghttp.VerifyRequest("PUT", "/result"),
								ghttp.VerifyJSONRepresenting(api.ContainerRunResult{
									Guid:          containerGuid,
									Failed:        true,
									FailureReason: "process error: because i said so",
									Result:        "",
								}),
							),
						)

						err := executorClient.Run(
							containerGuid,
							api.ContainerRunRequest{
								Actions: []models.ExecutorAction{
									{
										models.RunAction{
											Path: "ls",
											Args: []string{"-al"},
										},
									},
								},
								CompleteURL: callbackHandler.URL() + "/result",
							},
						)

						Ω(err).ShouldNot(HaveOccurred())
					})

					AfterEach(func() {
						callbackHandler.Close()
					})

					It("invokes the callback with failed true and a reason", func() {
						Eventually(callbackHandler.ReceivedRequests).Should(HaveLen(1))
					})
				})
			})
		})

		Describe("deleting a container", func() {
			var deleteErr error
			var guid string

			JustBeforeEach(func() {
				deleteErr = executorClient.DeleteContainer(guid)
			})

			Context("when the container has been allocated", func() {
				BeforeEach(func() {
					guid, _ = allocNewContainer(api.ContainerAllocationRequest{
						MemoryMB: 1024,
						DiskMB:   1024,
					})
				})

				It("the previously allocated resources become available", func() {
					Eventually(executorClient.RemainingResources).Should(Equal(api.ExecutorResources{
						MemoryMB:   1024,
						DiskMB:     1024,
						Containers: 1024,
					}))
				})
			})

			Context("when the container has been initialized", func() {
				BeforeEach(func() {
					guid, _ = initNewContainer()
				})

				Context("when deleting the container succeeds", func() {
					It("destroys the warden container", func() {
						Eventually(fakeBackend.DestroyCallCount).Should(Equal(1))
						Ω(fakeBackend.DestroyArgsForCall(0)).Should(Equal("some-handle"))
					})

					It("removes the container from the registry", func() {
						_, err := executorClient.GetContainer(guid)
						Ω(err).Should(Equal(api.ErrContainerNotFound))
					})
				})

				Context("when deleting the container fails", func() {
					BeforeEach(func() {
						fakeBackend.DestroyReturns(errors.New("oh no!"))
					})

					It("returns an error", func() {
						Ω(deleteErr.Error()).Should(ContainSubstring("status: 500"))
					})
				})
			})

			Context("when another delete is in progress", func() {
				var destroying, conflicted chan struct{}
				var errChan chan error

				BeforeEach(func() {
					guid, _ = initNewContainer()

					destroying = make(chan struct{})
					conflicted = make(chan struct{})
					errChan = make(chan error)

					fakeBackend.DestroyStub = func(string) error {
						close(destroying)
						<-conflicted
						return nil
					}

					go func() {
						errChan <- executorClient.DeleteContainer(guid)
					}()

					Eventually(destroying).Should(BeClosed())
				})

				AfterEach(func() {
					close(conflicted)
					Eventually(errChan).Should(Receive(nil))
				})

				It("returns ErrDeleteInProgress", func() {
					Ω(deleteErr).Should(Equal(api.ErrDeleteInProgress))
				})

				It("does not delete the container twice", func() {
					Ω(fakeBackend.DestroyCallCount()).Should(Equal(1))
				})
			})

			Context("while it is running", func() {
				BeforeEach(func() {
					var fakeContainer *wfakes.FakeContainer

					guid, fakeContainer = initNewContainer()

					waiting := make(chan struct{})
					destroying := make(chan struct{})

					process := new(wfakes.FakeProcess)
					process.WaitStub = func() (int, error) {
						close(waiting)
						<-destroying
						return 123, nil
					}

					fakeContainer.RunReturns(process, nil)

					fakeBackend.DestroyStub = func(string) error {
						close(destroying)
						return nil
					}

					err := executorClient.Run(guid, api.ContainerRunRequest{
						Actions: []models.ExecutorAction{
							{Action: models.RunAction{Path: "ls"}},
						},
					})
					Ω(err).ShouldNot(HaveOccurred())

					Eventually(waiting).Should(BeClosed())
				})

				It("does not return an error (because of funky state transitions)", func() {
					Ω(deleteErr).ShouldNot(HaveOccurred())
				})

				It("deletes the container", func() {
					Eventually(fakeBackend.DestroyCallCount).Should(Equal(1))
					Ω(fakeBackend.DestroyArgsForCall(0)).Should(Equal("some-handle"))
				})

				It("removes the container from the registry", func() {
					_, err := executorClient.GetContainer(guid)
					Ω(err).Should(Equal(api.ErrContainerNotFound))
				})
			})
		})

		Describe("pruning the registry", func() {
			It("continually prunes the registry", func() {
				guid, err := uuid.NewV4()
				Ω(err).ShouldNot(HaveOccurred())

				_, err = executorClient.AllocateContainer(guid.String(), api.ContainerAllocationRequest{
					MemoryMB: 1024,
					DiskMB:   1024,
				})
				Ω(err).ShouldNot(HaveOccurred())

				Ω(executorClient.ListContainers()).Should(HaveLen(1))
				Eventually(executorClient.ListContainers, pruningInterval*3).Should(BeEmpty())
			})
		})

		Describe("when the executor receives the TERM signal", func() {
			It("exits successfully", func() {
				runner.Session.Terminate()
				Eventually(runner.Session, 2).Should(gexec.Exit())
			})
		})

		Describe("when the executor receives the INT signal", func() {
			It("exits successfully", func() {
				runner.Session.Interrupt()
				Eventually(runner.Session, 2).Should(gexec.Exit())
			})
		})

		It("serves debug information", func() {
			debugResponse, err := http.Get(fmt.Sprintf("http://%s/debug/pprof/goroutine", debugAddr))
			Ω(err).ShouldNot(HaveOccurred())

			debugInfo, err := ioutil.ReadAll(debugResponse.Body)
			Ω(err).ShouldNot(HaveOccurred())

			Ω(debugInfo).Should(ContainSubstring("goroutine profile: total"))
		})
	})

	Describe("when gardenserver is unavailable", func() {
		BeforeEach(func() {
			runner.StartWithoutCheck(executor_runner.Config{
				ContainerOwnerName: "executor-name",
			})
		})

		Context("and gardenserver starts up later", func() {
			var started chan struct{}
			BeforeEach(func() {
				started = make(chan struct{})
				go func() {
					time.Sleep(50 * time.Millisecond)
					err := gardenServer.Start()
					Ω(err).ShouldNot(HaveOccurred())
					close(started)
				}()
			})

			AfterEach(func() {
				<-started
			})

			It("should connect", func() {
				Eventually(runner.Session, 5*time.Second).Should(gbytes.Say("started"))
			})
		})

		Context("and never starts", func() {
			It("should not exit and continue waiting for a connection", func() {
				Consistently(runner.Session).ShouldNot(gbytes.Say("started"))
				Ω(runner.Session).ShouldNot(gexec.Exit())
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
