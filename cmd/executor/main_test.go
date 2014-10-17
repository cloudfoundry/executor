package main_test

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"syscall"
	"time"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/executor/cmd/executor/testrunner"
	"github.com/nu7hatch/gouuid"
	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/config"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/gexec"
	"github.com/onsi/gomega/ghttp"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/ginkgomon"

	"github.com/cloudfoundry-incubator/executor/http/client"
	gapi "github.com/cloudfoundry-incubator/garden/api"
	gfakes "github.com/cloudfoundry-incubator/garden/api/fakes"
	GardenServer "github.com/cloudfoundry-incubator/garden/server"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/pivotal-golang/lager/lagertest"
)

var gardenServer *GardenServer.GardenServer
var runner *ginkgomon.Runner
var executorClient executor.Client

var _ = Describe("Executor", func() {
	var (
		gardenAddr      string
		debugAddr       string
		containerHandle string = "some-handle"
		process         ifrit.Process
		cachePath       string
		tmpDir          string
		ownerName       string

		fakeBackend *gfakes.FakeBackend
	)

	pruningInterval := 500 * time.Millisecond

	BeforeEach(func() {
		gardenPort := 9001 + GinkgoParallelNode()
		debugAddr = fmt.Sprintf("127.0.0.1:%d", 10001+GinkgoParallelNode())
		executorAddr := fmt.Sprintf("127.0.0.1:%d", 1700+GinkgoParallelNode())

		executorClient = client.New(http.DefaultClient, "http://"+executorAddr)

		gardenAddr = fmt.Sprintf("127.0.0.1:%d", gardenPort)

		fakeBackend = new(gfakes.FakeBackend)
		fakeBackend.CapacityReturns(gapi.Capacity{
			MemoryInBytes: 1024 * 1024 * 1024,
			DiskInBytes:   1024 * 1024 * 1024,
			MaxContainers: 1024,
		}, nil)

		gardenServer = GardenServer.New("tcp", gardenAddr, 0, fakeBackend, lagertest.NewTestLogger("garden"))
		tmpDir = path.Join(os.TempDir(), fmt.Sprintf("executor_%d", GinkgoParallelNode()))
		cachePath = path.Join(tmpDir, "cache")
		ownerName = fmt.Sprintf("executor-on-node-%d", config.GinkgoConfig.ParallelNode)

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
		)
	})

	AfterEach(func() {
		if process != nil {
			process.Signal(os.Kill)
			Eventually(process.Wait()).Should(Receive())
		}
		gardenServer.Stop()

		Eventually(func() error {
			conn, err := net.Dial("tcp", gardenAddr)
			if err == nil {
				conn.Close()
			}

			return err
		}).Should(HaveOccurred())
	})

	allocNewContainer := func(request executor.ContainerAllocationRequest) (guid string, fakeContainer *gfakes.FakeContainer) {
		fakeContainer = new(gfakes.FakeContainer)
		fakeBackend.CreateReturns(fakeContainer, nil)
		fakeBackend.LookupReturns(fakeContainer, nil)
		fakeBackend.ContainersReturns([]gapi.Container{fakeContainer}, nil)
		fakeContainer.HandleReturns(containerHandle)

		id, err := uuid.NewV4()
		Ω(err).ShouldNot(HaveOccurred())

		guid = id.String()

		_, err = executorClient.AllocateContainer(guid, request)
		Ω(err).ShouldNot(HaveOccurred())

		return guid, fakeContainer
	}

	initNewContainer := func() (guid string, fakeContainer *gfakes.FakeContainer) {
		guid, fakeContainer = allocNewContainer(executor.ContainerAllocationRequest{
			MemoryMB: 1024,
			DiskMB:   1024,
		})

		index := 13
		_, err := executorClient.InitializeContainer(guid, executor.ContainerInitializationRequest{
			Log: executor.LogConfig{
				Guid:       "the-app-guid",
				SourceName: "STG",
				Index:      &index,
			},
		})
		Ω(err).ShouldNot(HaveOccurred())

		return guid, fakeContainer
	}

	Describe("starting up", func() {
		var workingDir string

		JustBeforeEach(func() {
			err := gardenServer.Start()
			Ω(err).ShouldNot(HaveOccurred())

			runner.StartCheck = ""
			process = ginkgomon.Invoke(runner)
		})

		BeforeEach(func() {
			workingDir = filepath.Join(tmpDir, "executor-work")
			os.RemoveAll(workingDir)
		})

		Context("when the working directory exists and contains files", func() {
			BeforeEach(func() {
				err := os.MkdirAll(workingDir, 0755)
				Ω(err).ShouldNot(HaveOccurred())

				err = ioutil.WriteFile(filepath.Join(workingDir, "should-get-deleted"), []byte("some-contents"), 0755)
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("cleans up its working directory", func() {
				Eventually(func() bool {
					files, err := ioutil.ReadDir(workingDir)
					if err != nil {
						return false
					}
					return len(files) == 0
				}).Should(BeTrue())
			})
		})

		Context("when the working directory doesn't exist", func() {
			It("creates a new working directory", func() {
				Eventually(func() bool {
					workingDirInfo, err := os.Stat(workingDir)
					if err != nil {
						return false
					}

					return workingDirInfo.IsDir()
				}).Should(BeTrue())
			})
		})

		Context("when there are containers that are owned by the executor", func() {
			var fakeContainer1, fakeContainer2 *gfakes.FakeContainer

			BeforeEach(func() {
				fakeContainer1 = new(gfakes.FakeContainer)
				fakeContainer1.HandleReturns("handle-1")

				fakeContainer2 = new(gfakes.FakeContainer)
				fakeContainer2.HandleReturns("handle-2")

				fakeBackend.ContainersStub = func(ps gapi.Properties) ([]gapi.Container, error) {
					if reflect.DeepEqual(ps, gapi.Properties{"owner": ownerName}) {
						return []gapi.Container{fakeContainer1, fakeContainer2}, nil
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

			Context("when deleting the container fails", func() {
				BeforeEach(func() {
					fakeBackend.DestroyReturns(errors.New("i tried to delete the thing but i failed. sorry."))
				})

				It("should exit sadly", func() {
					Eventually(runner, 5*time.Second).Should(gexec.Exit(2))
				})
			})
		})
	})

	Describe("when started", func() {
		BeforeEach(func() {
			err := gardenServer.Start()
			Ω(err).ShouldNot(HaveOccurred())

			process = ginkgomon.Invoke(runner)
		})

		Describe("pinging the server", func() {
			var pingErr error

			JustBeforeEach(func() {
				pingErr = executorClient.Ping()
			})

			Context("when Garden responds to ping", func() {
				BeforeEach(func() {
					fakeBackend.PingReturns(nil)
				})

				It("not return an error", func() {
					Ω(pingErr).ShouldNot(HaveOccurred())
				})
			})

			Context("when Garden returns an error", func() {
				BeforeEach(func() {
					fakeBackend.PingReturns(errors.New("oh no!"))
				})

				It("should return an error", func() {
					Ω(pingErr.Error()).Should(ContainSubstring("status: 502"))
				})
			})
		})

		Describe("getting the total resources", func() {
			var resources executor.ExecutorResources
			var resourceErr error
			JustBeforeEach(func() {
				resources, resourceErr = executorClient.TotalResources()
			})

			It("not return an error", func() {
				Ω(resourceErr).ShouldNot(HaveOccurred())
			})

			It("returns the preset capacity", func() {
				expectedResources := executor.ExecutorResources{
					MemoryMB:   1024,
					DiskMB:     1024,
					Containers: 1024,
				}
				Ω(resources).Should(Equal(expectedResources))
			})
		})

		Describe("allocating the container", func() {
			var guid string
			var allocatedContainer executor.Container
			var allocErr error

			BeforeEach(func() {
				id, err := uuid.NewV4()
				Ω(err).ShouldNot(HaveOccurred())
				guid = id.String()
			})

			JustBeforeEach(func() {
				allocatedContainer, allocErr = executorClient.AllocateContainer(guid, executor.ContainerAllocationRequest{
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
					var container executor.Container
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
						Ω(executorClient.RemainingResources()).Should(Equal(executor.ExecutorResources{
							MemoryMB:   768,
							DiskMB:     768,
							Containers: 1023,
						}))
					})
				})
			})

			Context("when the container cannot be reserved because the guid is already taken", func() {
				BeforeEach(func() {
					_, err := executorClient.AllocateContainer(guid, executor.ContainerAllocationRequest{
						MemoryMB: 1,
						DiskMB:   1,
					})
					Ω(err).ShouldNot(HaveOccurred())
				})

				It("returns an error", func() {
					Ω(allocErr).Should(Equal(executor.ErrContainerGuidNotAvailable))
				})
			})

			Context("when the container cannot be reserved because there is no room", func() {
				BeforeEach(func() {
					_, err := executorClient.AllocateContainer("another-container-guid", executor.ContainerAllocationRequest{
						MemoryMB: 1024,
						DiskMB:   1024,
					})
					Ω(err).ShouldNot(HaveOccurred())
				})

				It("returns an error", func() {
					Ω(allocErr).Should(Equal(executor.ErrInsufficientResourcesAvailable))
				})
			})
		})

		Describe("initializing the container", func() {
			var initializeContainerRequest executor.ContainerInitializationRequest
			var err error
			var initializedContainer executor.Container
			var container *gfakes.FakeContainer
			var guid string

			BeforeEach(func() {
				guid, container = allocNewContainer(executor.ContainerAllocationRequest{
					MemoryMB: 1024,
					DiskMB:   1024,
				})

				initializeContainerRequest = executor.ContainerInitializationRequest{
					CpuPercent: 50.0,
				}
			})

			JustBeforeEach(func() {
				initializedContainer, err = executorClient.InitializeContainer(guid, initializeContainerRequest)
			})

			Context("when the requested CPU percent is > 100", func() {
				BeforeEach(func() {
					initializeContainerRequest = executor.ContainerInitializationRequest{
						CpuPercent: 101.0,
					}
				})

				It("returns an error", func() {
					Ω(err).Should(HaveOccurred())
					Ω(err).Should(Equal(executor.ErrLimitsInvalid))
				})
			})

			Context("when the requested CPU percent is < 0", func() {
				BeforeEach(func() {
					initializeContainerRequest = executor.ContainerInitializationRequest{
						CpuPercent: -14.0,
					}
				})

				It("returns an error", func() {
					Ω(err).Should(HaveOccurred())
					Ω(err).Should(Equal(executor.ErrLimitsInvalid))
				})
			})

			Context("when the container specifies a docker root_fs", func() {
				var expectedRootFS = "docker:///docker.com/my-image"

				BeforeEach(func() {
					initializeContainerRequest = executor.ContainerInitializationRequest{
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
						guid, container = allocNewContainer(executor.ContainerAllocationRequest{
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
						initializeContainerRequest = executor.ContainerInitializationRequest{
							CpuPercent: 0.5,
							Ports: []executor.PortMapping{
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
							Ω(initializedContainer.Ports).Should(Equal([]executor.PortMapping{
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
						initializeContainerRequest = executor.ContainerInitializationRequest{
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
			It("propagates global environment variables to each run action", func() {
				guid, fakeContainer := initNewContainer()

				process := new(gfakes.FakeProcess)
				fakeContainer.RunReturns(process, nil)

				err := executorClient.Run(
					guid,
					executor.ContainerRunRequest{
						Env: []executor.EnvironmentVariable{
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
				var fakeContainer *gfakes.FakeContainer

				BeforeEach(func() {
					callbackHandler = ghttp.NewServer()

					containerGuid, fakeContainer = initNewContainer()

					process := new(gfakes.FakeProcess)
					fakeContainer.RunReturns(process, nil)

					err := executorClient.Run(
						containerGuid,
						executor.ContainerRunRequest{
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
								ghttp.VerifyJSONRepresenting(executor.ContainerRunResult{
									Guid:          containerGuid,
									Failed:        false,
									FailureReason: "",
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
						Ω(container.State).Should(Equal(executor.StateCompleted))
					})

					It("does not free the container's reserved resources", func() {
						Consistently(executorClient.RemainingResources).Should(Equal(executor.ExecutorResources{
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
				var fakeContainer *gfakes.FakeContainer
				var containerGuid string

				BeforeEach(func() {
					containerGuid, fakeContainer = initNewContainer()
					process := new(gfakes.FakeProcess)
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
								ghttp.VerifyJSONRepresenting(executor.ContainerRunResult{
									Guid:          containerGuid,
									Failed:        true,
									FailureReason: "process error: because i said so",
								}),
							),
						)

						err := executorClient.Run(
							containerGuid,
							executor.ContainerRunRequest{
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
			var guid string

			Context("when the container has been allocated", func() {
				BeforeEach(func() {
					guid, _ = allocNewContainer(executor.ContainerAllocationRequest{
						MemoryMB: 1024,
						DiskMB:   1024,
					})
				})

				It("makes the previously allocated resources available again", func() {
					err := executorClient.DeleteContainer(guid)
					Ω(err).ShouldNot(HaveOccurred())

					Eventually(executorClient.RemainingResources).Should(Equal(executor.ExecutorResources{
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
					It("destroys the garden container", func() {
						err := executorClient.DeleteContainer(guid)
						Ω(err).ShouldNot(HaveOccurred())

						Eventually(fakeBackend.DestroyCallCount).Should(Equal(1))
						Ω(fakeBackend.DestroyArgsForCall(0)).Should(Equal(containerHandle))
					})

					It("removes the container from the registry", func() {
						err := executorClient.DeleteContainer(guid)
						Ω(err).ShouldNot(HaveOccurred())

						_, err = executorClient.GetContainer(guid)
						Ω(err).Should(Equal(executor.ErrContainerNotFound))
					})
				})

				Context("when deleting the container fails", func() {
					BeforeEach(func() {
						fakeBackend.DestroyReturns(errors.New("oh no!"))
					})

					It("returns an error", func() {
						err := executorClient.DeleteContainer(guid)
						Ω(err.Error()).Should(ContainSubstring("status: 500"))
					})

					Describe("retrying", func() {
						BeforeEach(func() {
							executorClient.DeleteContainer(guid)
							fakeBackend.DestroyReturns(nil)
						})

						It("tries to delete the garden container again", func() {
							err := executorClient.DeleteContainer(guid)
							Ω(fakeBackend.DestroyCallCount()).Should(Equal(2))
							Ω(err).ShouldNot(HaveOccurred())
						})
					})
				})
			})

			Context("while it is running", func() {
				BeforeEach(func() {
					var fakeContainer *gfakes.FakeContainer

					guid, fakeContainer = initNewContainer()

					waiting := make(chan struct{})
					destroying := make(chan struct{})

					process := new(gfakes.FakeProcess)
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

					err := executorClient.Run(guid, executor.ContainerRunRequest{
						Actions: []models.ExecutorAction{
							{Action: models.RunAction{Path: "ls"}},
						},
					})
					Ω(err).ShouldNot(HaveOccurred())

					Eventually(waiting).Should(BeClosed())
				})

				It("does not return an error", func() {
					err := executorClient.DeleteContainer(guid)
					Ω(err).ShouldNot(HaveOccurred())
				})

				It("deletes the container", func() {
					executorClient.DeleteContainer(guid)

					Eventually(fakeBackend.DestroyCallCount).Should(Equal(1))
					Ω(fakeBackend.DestroyArgsForCall(0)).Should(Equal(containerHandle))
				})

				It("removes the container from the registry", func() {
					executorClient.DeleteContainer(guid)

					_, err := executorClient.GetContainer(guid)
					Ω(err).Should(Equal(executor.ErrContainerNotFound))
				})
			})
		})

		Describe("getting files from a container", func() {
			var (
				guid string

				stream    io.ReadCloser
				streamErr error
			)

			JustBeforeEach(func() {
				stream, streamErr = executorClient.GetFiles(guid, "some/path")
			})

			Context("when the container hasn't been initialized", func() {
				BeforeEach(func() {
					guid, _ = allocNewContainer(executor.ContainerAllocationRequest{
						MemoryMB: 1024,
						DiskMB:   1024,
					})
				})

				It("returns an error", func() {
					Ω(streamErr).Should(HaveOccurred())
				})
			})

			Context("when the container has been initialized", func() {
				var fakeContainer *gfakes.FakeContainer

				BeforeEach(func() {
					guid, fakeContainer = initNewContainer()
				})

				Context("when streaming out the file succeeds", func() {
					var (
						streamedOut *gbytes.Buffer
					)

					BeforeEach(func() {
						streamedOut = gbytes.BufferWithBytes([]byte{1, 2, 3})

						fakeContainer.StreamOutReturns(streamedOut, nil)
					})

					It("does not error", func() {
						Ω(streamErr).ShouldNot(HaveOccurred())
					})

					It("streams out the requested path", func() {
						requestedPath := fakeContainer.StreamOutArgsForCall(0)
						Ω(requestedPath).Should(Equal("some/path"))
					})

					It("returns a stream of the contents of the file", func() {
						contents, err := ioutil.ReadAll(stream)
						Ω(err).ShouldNot(HaveOccurred())
						Ω(contents).Should(Equal(streamedOut.Contents()))
					})
				})

				Context("when streaming out the file fails", func() {
					BeforeEach(func() {
						fakeContainer.StreamOutReturns(nil, errors.New("oh no!"))
					})

					It("returns an error", func() {
						Ω(streamErr).Should(HaveOccurred())
						Ω(streamErr.Error()).Should(ContainSubstring("status: 500"))
					})
				})
			})
		})

		Describe("staying in sync with garden", func() {
			Context("when a created container disappears out from under us", func() {
				var guid string

				BeforeEach(func() {
					guid, _ = initNewContainer()

					fakeBackend.ContainersReturns([]gapi.Container{}, nil)
				})

				It("returns a not-found response when that container is queried", func() {
					_, err := executorClient.GetContainer(guid)
					Ω(err).Should(Equal(executor.ErrContainerNotFound))
				})

				It("returns a not-found error when that container is deleted", func() {
					err := executorClient.DeleteContainer(guid)
					Ω(err).Should(Equal(executor.ErrContainerNotFound))
				})

				It("returns a not-found error when attempting to run something in that container", func() {
					err := executorClient.Run(guid, executor.ContainerRunRequest{})
					Ω(err).Should(Equal(executor.ErrContainerNotFound))
				})

				It("does not return that container when listing containers", func() {
					Ω(executorClient.ListContainers()).Should(BeEmpty())
				})

				It("frees that container's resources from the available resources", func() {
					Ω(executorClient.RemainingResources()).Should(Equal(executor.ExecutorResources{
						MemoryMB:   1024,
						DiskMB:     1024,
						Containers: 1024,
					}))
				})

				It("makes that container's resources available for subsequent allocations", func() {
					_, err := executorClient.AllocateContainer("the-guid", executor.ContainerAllocationRequest{
						MemoryMB: 1024,
					})

					Ω(err).ShouldNot(HaveOccurred())
				})
			})
		})

		Describe("pruning the registry", func() {
			It("continually prunes the registry", func() {
				guid, err := uuid.NewV4()
				Ω(err).ShouldNot(HaveOccurred())

				_, err = executorClient.AllocateContainer(guid.String(), executor.ContainerAllocationRequest{
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
				process.Signal(syscall.SIGTERM)
				Eventually(runner, 2).Should(gexec.Exit())
			})
		})

		Describe("when the executor receives the INT signal", func() {
			It("exits successfully", func() {
				process.Signal(syscall.SIGINT)
				Eventually(runner, 2).Should(gexec.Exit())
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
			runner.StartCheck = ""
			process = ginkgomon.Invoke(runner)
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
				Eventually(runner.Buffer(), 5*time.Second).Should(gbytes.Say("started"))
			})
		})

		Context("and never starts", func() {
			It("should not exit and continue waiting for a connection", func() {
				Consistently(runner.Buffer()).ShouldNot(gbytes.Say("started"))
				Ω(runner).ShouldNot(gexec.Exit())
			})
		})
	})
})
