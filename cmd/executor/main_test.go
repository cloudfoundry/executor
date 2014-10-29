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
	"sync"
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
	garden "github.com/cloudfoundry-incubator/garden/api"
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
		fakeBackend.CapacityReturns(garden.Capacity{
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

	generateGuid := func() string {
		id, err := uuid.NewV4()
		Ω(err).ShouldNot(HaveOccurred())

		return id.String()
	}

	allocNewContainer := func(request executor.Container) string {
		guid := generateGuid()

		_, err := executorClient.AllocateContainer(guid, request)
		Ω(err).ShouldNot(HaveOccurred())

		return guid
	}

	setupFakeContainer := func() *gfakes.FakeContainer {
		fakeContainer := new(gfakes.FakeContainer)
		fakeContainer.HandleReturns(containerHandle)

		fakeBackend.CreateReturns(fakeContainer, nil)
		fakeBackend.LookupReturns(fakeContainer, nil)
		fakeBackend.ContainersReturns([]garden.Container{fakeContainer}, nil)

		return fakeContainer
	}

	getContainer := func(guid string) executor.Container {
		container, err := executorClient.GetContainer(guid)
		Ω(err).ShouldNot(HaveOccurred())

		return container
	}

	Describe("starting up", func() {
		var workingDir string

		JustBeforeEach(func() {
			err := gardenServer.Start()
			Ω(err).ShouldNot(HaveOccurred())

			runner.StartCheck = ""

			// start without check; some things make it fail on start
			process = ifrit.Invoke(runner)
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

				fakeBackend.ContainersStub = func(ps garden.Properties) ([]garden.Container, error) {
					if reflect.DeepEqual(ps, garden.Properties{"executor:owner": ownerName}) {
						return []garden.Container{fakeContainer1, fakeContainer2}, nil
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

		Describe("allocating a container", func() {
			var (
				container executor.Container

				guid string

				allocatedContainer executor.Container
				allocErr           error
			)

			BeforeEach(func() {
				guid = generateGuid()

				container = executor.Container{
					Tags: executor.Tags{"some-tag": "some-value"},

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
				}
			})

			JustBeforeEach(func() {
				allocatedContainer, allocErr = executorClient.AllocateContainer(guid, container)
			})

			It("does not return an error", func() {
				Ω(allocErr).ShouldNot(HaveOccurred())
			})

			It("returns a container", func() {
				Ω(allocatedContainer.Guid).Should(Equal(guid))
				Ω(allocatedContainer.MemoryMB).Should(Equal(0))
				Ω(allocatedContainer.DiskMB).Should(Equal(0))
				Ω(allocatedContainer.Tags).Should(Equal(executor.Tags{"some-tag": "some-value"}))
				Ω(allocatedContainer.State).Should(Equal(executor.StateReserved))
				Ω(allocatedContainer.AllocatedAt).Should(BeNumerically("~", time.Now().UnixNano(), time.Second))
			})

			Context("when allocated with memory and disk limits", func() {
				BeforeEach(func() {
					container.MemoryMB = 256
					container.DiskMB = 256
				})

				It("returns the limits on the container", func() {
					Ω(allocatedContainer.MemoryMB).Should(Equal(256))
					Ω(allocatedContainer.DiskMB).Should(Equal(256))
				})

				It("reduces the capacity by the amount reserved", func() {
					Ω(executorClient.RemainingResources()).Should(Equal(executor.ExecutorResources{
						MemoryMB:   768,
						DiskMB:     768,
						Containers: 1023,
					}))
				})
			})

			Context("when the requested CPU weight is > 100", func() {
				BeforeEach(func() {
					container.CPUWeight = 101
				})

				It("returns an error", func() {
					Ω(allocErr).Should(HaveOccurred())
					Ω(allocErr).Should(Equal(executor.ErrLimitsInvalid))
				})
			})

			Context("when the guid is already taken", func() {
				BeforeEach(func() {
					_, err := executorClient.AllocateContainer(guid, container)
					Ω(err).ShouldNot(HaveOccurred())
				})

				It("returns an error", func() {
					Ω(allocErr).Should(Equal(executor.ErrContainerGuidNotAvailable))
				})
			})

			Context("when there is no room", func() {
				BeforeEach(func() {
					container.MemoryMB = 999999999999999
					container.DiskMB = 999999999999999
				})

				It("returns an error", func() {
					Ω(allocErr).Should(Equal(executor.ErrInsufficientResourcesAvailable))
				})
			})

			Describe("running it", func() {
				var runErr error

				JustBeforeEach(func() {
					runErr = executorClient.RunContainer(guid)
				})

				itCompletesWithFailure := func(reason string) {
					It("eventually completes with failure", func() {
						var container executor.Container

						Eventually(func() executor.State {
							container = getContainer(guid)
							return container.State
						}, 5*time.Second, 1*time.Second).Should(Equal(executor.StateCompleted))

						Ω(container.RunResult.Failed).Should(BeTrue())
						Ω(container.RunResult.FailureReason).Should(Equal(reason))
					})
				}

				Context("when the container can be created", func() {
					var gardenContainer *gfakes.FakeContainer

					BeforeEach(func() {
						gardenContainer = fakeGardenContainerWithProperties()

						gardenContainer.HandleReturns(containerHandle)

						fakeBackend.LookupReturns(gardenContainer, nil)
						fakeBackend.ContainersReturns([]garden.Container{gardenContainer}, nil)

						fakeBackend.CreateReturns(gardenContainer, nil)

						process := new(gfakes.FakeProcess)
						process.WaitReturns(0, nil)

						gardenContainer.RunReturns(process, nil)
					})

					It("returns no error", func() {
						Ω(runErr).ShouldNot(HaveOccurred())
					})

					It("creates it with the tags as namespaced properties", func() {
						Eventually(fakeBackend.CreateCallCount).Should(Equal(1))

						created := fakeBackend.CreateArgsForCall(0)
						Ω(created.Properties).Should(HaveKeyWithValue("tag:some-tag", "some-value"))
					})

					It("creates it with the configured owner", func() {
						Eventually(fakeBackend.CreateCallCount).Should(Equal(1))

						created := fakeBackend.CreateArgsForCall(0)
						ownerName := fmt.Sprintf("executor-on-node-%d", config.GinkgoConfig.ParallelNode)
						Ω(created.Properties["executor:owner"]).Should(Equal(ownerName))
					})

					It("propagates global environment variables to each run action", func() {
						Eventually(gardenContainer.RunCallCount, 10).Should(Equal(1))

						spec, _ := gardenContainer.RunArgsForCall(0)
						Ω(spec.Path).Should(Equal("ls"))
						Ω(spec.Env).Should(Equal([]string{
							"ENV1=val1",
							"ENV2=val2",
							"RUN_ENV1=run_val1",
							"RUN_ENV2=run_val2",
						}))
					})

					It("saves the succeeded run result", func() {
						var container executor.Container

						Eventually(func() executor.State {
							container = getContainer(guid)
							return container.State
						}).Should(Equal(executor.StateCompleted))

						Ω(container.RunResult.Failed).Should(BeFalse())
						Ω(container.RunResult.FailureReason).Should(BeEmpty())
					})

					Context("when created with memory, disk, cpu, and inode limits", func() {
						BeforeEach(func() {
							container.MemoryMB = 256
							container.DiskMB = 256
							container.CPUWeight = 50
						})

						It("applies them to the container", func() {
							Eventually(gardenContainer.LimitMemoryCallCount).Should(Equal(1))
							Eventually(gardenContainer.LimitDiskCallCount).Should(Equal(1))
							Eventually(gardenContainer.LimitCPUCallCount).Should(Equal(1))

							limitedMemory := gardenContainer.LimitMemoryArgsForCall(0)
							Ω(limitedMemory.LimitInBytes).Should(Equal(uint64(256 * 1024 * 1024)))

							limitedDisk := gardenContainer.LimitDiskArgsForCall(0)
							Ω(limitedDisk.ByteHard).Should(Equal(uint64(256 * 1024 * 1024)))
							Ω(limitedDisk.InodeHard).Should(Equal(uint64(245000)))

							limitedCPU := gardenContainer.LimitCPUArgsForCall(0)
							Ω(limitedCPU.LimitInShares).Should(Equal(uint64(512)))
						})
					})

					Context("when the container was allocated with a rootfs", func() {
						var expectedRootFS = "docker:///docker.com/my-image"

						BeforeEach(func() {
							container.RootFSPath = expectedRootFS
						})

						It("creates it with the configured rootfs", func() {
							Eventually(fakeBackend.CreateCallCount).Should(Equal(1))

							created := fakeBackend.CreateArgsForCall(0)
							Ω(created.RootFSPath).Should(Equal(expectedRootFS))
						})
					})

					Context("when the container has no memory limits requested", func() {
						BeforeEach(func() {
							container.MemoryMB = 0
						})

						It("does not enforce a zero-value memory limit", func() {
							Consistently(gardenContainer.LimitMemoryCallCount).Should(Equal(0))
						})
					})

					Context("when the container has no disk limits requested", func() {
						BeforeEach(func() {
							container.DiskMB = 0
						})

						It("enforces the inode limit, and a zero-value byte limit (which means unlimited)", func() {
							Eventually(gardenContainer.LimitDiskCallCount).Should(Equal(1))

							limitedDisk := gardenContainer.LimitDiskArgsForCall(0)
							Ω(limitedDisk.ByteHard).Should(BeZero())
							Ω(limitedDisk.InodeHard).Should(Equal(uint64(245000)))
						})
					})

					Context("when the container has no CPU limits requested", func() {
						BeforeEach(func() {
							container.CPUWeight = 0
						})

						It("does not enforce a zero-value limit", func() {
							Eventually(gardenContainer.LimitCPUCallCount).Should(Equal(0))
						})
					})

					Context("when ports are exposed", func() {
						BeforeEach(func() {
							container.Ports = []executor.PortMapping{
								{ContainerPort: 8080, HostPort: 0},
								{ContainerPort: 8081, HostPort: 1234},
							}
						})

						It("exposes the configured ports", func() {
							Eventually(gardenContainer.NetInCallCount).Should(Equal(2))

							netInH, netInC := gardenContainer.NetInArgsForCall(0)
							Ω(netInH).Should(Equal(uint32(0)))
							Ω(netInC).Should(Equal(uint32(8080)))

							netInH, netInC = gardenContainer.NetInArgsForCall(1)
							Ω(netInH).Should(Equal(uint32(1234)))
							Ω(netInC).Should(Equal(uint32(8081)))
						})
					})

					Context("and there is a complete callback", func() {
						var callbackHandler *ghttp.Server

						BeforeEach(func() {
							callbackHandler = ghttp.NewServer()
							container.CompleteURL = callbackHandler.URL() + "/result"

							callbackHandler.AppendHandlers(
								ghttp.CombineHandlers(
									ghttp.VerifyRequest("PUT", "/result"),
									ghttp.VerifyJSONRepresenting(executor.ContainerRunResult{
										Guid:   guid,
										Failed: false,
									}),
								),
							)
						})

						It("invokes the callback with failed false", func() {
							Eventually(callbackHandler.ReceivedRequests).Should(HaveLen(1))
						})

						Context("and the callback fails", func() {
							BeforeEach(func() {
								callbackHandler.SetHandler(
									0,
									http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
										callbackHandler.HTTPTestServer.CloseClientConnections()
									}),
								)

								callbackHandler.AppendHandlers(
									ghttp.RespondWith(http.StatusInternalServerError, ""),
									ghttp.RespondWith(http.StatusOK, ""),
								)
							})

							It("invokes the callback repeatedly", func() {
								Eventually(callbackHandler.ReceivedRequests, 5).Should(HaveLen(3))
							})
						})
					})

					Context("when running fails", func() {
						BeforeEach(func() {
							process := new(gfakes.FakeProcess)
							process.WaitReturns(1, nil)

							gardenContainer.RunReturns(process, nil)
						})

						It("saves the failed result and reason", func() {
							var container executor.Container

							Eventually(func() executor.State {
								container = getContainer(guid)
								return container.State
							}).Should(Equal(executor.StateCompleted))

							Ω(container.RunResult.Failed).Should(BeTrue())
							Ω(container.RunResult.FailureReason).Should(Equal("Exited with status 1"))
						})

						Context("and there is a complete callback", func() {
							var callbackHandler *ghttp.Server

							BeforeEach(func() {
								callbackHandler = ghttp.NewServer()
								container.CompleteURL = callbackHandler.URL() + "/result"

								callbackHandler.AppendHandlers(
									ghttp.CombineHandlers(
										ghttp.VerifyRequest("PUT", "/result"),
										ghttp.VerifyJSONRepresenting(executor.ContainerRunResult{
											Guid:          guid,
											Failed:        true,
											FailureReason: "Exited with status 1",
										}),
									),
								)
							})

							It("invokes the callback with failed true and a reason", func() {
								Eventually(callbackHandler.ReceivedRequests).Should(HaveLen(1))
							})
						})
					})
				})

				Context("when the container cannot be created", func() {
					BeforeEach(func() {
						fakeBackend.CreateReturns(nil, errors.New("oh no!"))
					})

					It("does not immediately return an error", func() {
						Ω(runErr).ShouldNot(HaveOccurred())
					})

					itCompletesWithFailure("failed to initialize container: oh no!")
				})
			})
		})

		Describe("running a bogus guid", func() {
			It("returns an error", func() {
				err := executorClient.RunContainer("bogus")
				Ω(err).Should(Equal(executor.ErrContainerNotFound))
			})
		})

		Describe("deleting a container", func() {
			var guid string

			Context("when the container has been allocated", func() {
				BeforeEach(func() {
					guid = allocNewContainer(executor.Container{
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

			Context("while the container is initializing", func() {
				var createdContainer chan struct{}

				BeforeEach(func() {
					guid = allocNewContainer(executor.Container{})

					fakeContainer := new(gfakes.FakeContainer)
					fakeContainer.HandleReturns(containerHandle)

					creating := make(chan struct{})

					createdContainer = make(chan struct{})

					fakeBackend.CreateStub = func(garden.ContainerSpec) (garden.Container, error) {
						close(creating)
						<-createdContainer

						fakeBackend.LookupReturns(fakeContainer, nil)
						fakeBackend.ContainersReturns([]garden.Container{fakeContainer}, nil)

						return fakeContainer, nil
					}

					err := executorClient.RunContainer(guid)
					Ω(err).ShouldNot(HaveOccurred())

					Eventually(creating).Should(BeClosed())
				})

				AfterEach(func() {
					select {
					case <-createdContainer:
					default:
						// un-hang garden
						close(createdContainer)
					}
				})

				It("destroys the garden container after creating it", func() {
					err := executorClient.DeleteContainer(guid)
					Ω(err).ShouldNot(HaveOccurred())

					Consistently(fakeBackend.DestroyCallCount).Should(BeZero())

					close(createdContainer)

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

			Context("while it is running", func() {
				BeforeEach(func() {
					guid = allocNewContainer(executor.Container{
						Actions: []models.ExecutorAction{
							{Action: models.RunAction{Path: "ls"}},
						},
					})

					fakeContainer := setupFakeContainer()

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

					err := executorClient.RunContainer(guid)
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

				It("removes the container from the API", func() {
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
					guid = allocNewContainer(executor.Container{
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
					guid = allocNewContainer(executor.Container{
						Actions: []models.ExecutorAction{
							{
								models.RunAction{
									Path: "ls",
								},
							},
						},
					})

					fakeContainer = setupFakeContainer()

					process := new(gfakes.FakeProcess)
					fakeContainer.RunReturns(process, nil)

					err := executorClient.RunContainer(guid)
					Ω(err).ShouldNot(HaveOccurred())

					Eventually(fakeContainer.RunCallCount).Should(Equal(1))
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

		Describe("pruning the registry", func() {
			It("continually prunes the registry", func() {
				guid, err := uuid.NewV4()
				Ω(err).ShouldNot(HaveOccurred())

				_, err = executorClient.AllocateContainer(guid.String(), executor.Container{
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

func fakeGardenContainerWithProperties() *gfakes.FakeContainer {
	properties := make(garden.Properties)
	propertyMu := new(sync.RWMutex)

	return &gfakes.FakeContainer{
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

			return garden.ContainerInfo{
				Properties: properties,
			}, nil
		},
	}
}
