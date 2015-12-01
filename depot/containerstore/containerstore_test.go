package containerstore_test

import (
	"bytes"
	"errors"
	"io/ioutil"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/ginkgomon"

	"github.com/cloudfoundry-incubator/bbs/models"
	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/executor/depot/containerstore"
	"github.com/cloudfoundry-incubator/executor/depot/transformer/faketransformer"
	"github.com/cloudfoundry-incubator/garden"
	"github.com/pivotal-golang/clock/fakeclock"
	"github.com/pivotal-golang/lager/lagertest"

	eventfakes "github.com/cloudfoundry-incubator/executor/depot/event/fakes"
	stepfakes "github.com/cloudfoundry-incubator/executor/depot/steps/fakes"
	gfakes "github.com/cloudfoundry-incubator/garden/fakes"
)

var _ = Describe("Container Store", func() {
	var (
		containerStore containerstore.ContainerStore

		iNodeLimit    uint64
		maxCPUShares  uint64
		ownerName     string
		totalCapacity executor.ExecutorResources

		containerGuid string

		gardenClient    *gfakes.FakeClient
		gardenContainer *gfakes.FakeContainer
		megatron        *faketransformer.FakeTransformer

		clock        *fakeclock.FakeClock
		eventEmitter *eventfakes.FakeHub

		logger *lagertest.TestLogger
	)

	BeforeEach(func() {
		gardenContainer = &gfakes.FakeContainer{}
		gardenClient = &gfakes.FakeClient{}
		clock = fakeclock.NewFakeClock(time.Now())
		eventEmitter = &eventfakes.FakeHub{}
		logger = lagertest.NewTestLogger("test-container-store")

		iNodeLimit = 64
		maxCPUShares = 100
		ownerName = "test-owner"
		totalCapacity = executor.NewExecutorResources(1024*10, 1024*10, 10)

		containerGuid = "container-guid"

		megatron = &faketransformer.FakeTransformer{}
		containerStore = containerstore.New(
			ownerName,
			iNodeLimit,
			maxCPUShares,
			&totalCapacity,
			gardenClient,
			clock,
			eventEmitter,
			megatron,
		)
	})

	Describe("Reserve", func() {
		var (
			containerTags     executor.Tags
			containerResource executor.Resource
			req               *executor.AllocationRequest
		)

		BeforeEach(func() {
			containerTags = executor.Tags{
				"Foo": "bar",
			}
			containerResource = executor.Resource{
				MemoryMB:   1024,
				DiskMB:     1024,
				RootFSPath: "/foo/bar",
			}
			req = &executor.AllocationRequest{
				Guid:     containerGuid,
				Tags:     containerTags,
				Resource: containerResource,
			}
		})

		It("returns a populated container", func() {
			container, err := containerStore.Reserve(logger, req)
			Expect(err).NotTo(HaveOccurred())

			Expect(container.Guid).To(Equal(containerGuid))
			Expect(container.Tags).To(Equal(containerTags))
			Expect(container.Resource).To(Equal(containerResource))
			Expect(container.State).To(Equal(executor.StateReserved))
			Expect(container.AllocatedAt).To(Equal(clock.Now().UnixNano()))
		})

		It("tracks the container", func() {
			container, err := containerStore.Reserve(logger, req)
			Expect(err).NotTo(HaveOccurred())

			found, err := containerStore.Get(logger, container.Guid)
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(Equal(container))
		})

		It("emits a reserved container event", func() {
			container, err := containerStore.Reserve(logger, req)
			Expect(err).NotTo(HaveOccurred())

			Eventually(eventEmitter.EmitCallCount).Should(Equal(1))

			event := eventEmitter.EmitArgsForCall(0)
			Expect(event).To(Equal(executor.ContainerReservedEvent{
				RawContainer: container,
			}))
		})

		It("decrements the remaining capacity", func() {
			_, err := containerStore.Reserve(logger, req)
			Expect(err).NotTo(HaveOccurred())

			remainingCapacity := containerStore.RemainingResources(logger)
			Expect(remainingCapacity.MemoryMB).To(Equal(totalCapacity.MemoryMB - req.MemoryMB))
			Expect(remainingCapacity.DiskMB).To(Equal(totalCapacity.DiskMB - req.DiskMB))
			Expect(remainingCapacity.Containers).To(Equal(totalCapacity.Containers - 1))
		})

		Context("when the container guid is already reserved", func() {
			BeforeEach(func() {
				_, err := containerStore.Reserve(logger, req)
				Expect(err).NotTo(HaveOccurred())
			})

			It("fails with container guid not available", func() {
				_, err := containerStore.Reserve(logger, req)
				Expect(err).To(Equal(executor.ErrContainerGuidNotAvailable))
			})
		})

		Context("when there are not enough remaining resources available", func() {
			BeforeEach(func() {
				req.Resource.MemoryMB = totalCapacity.MemoryMB + 1
			})

			It("returns an error", func() {
				_, err := containerStore.Reserve(logger, req)
				Expect(err).To(Equal(executor.ErrInsufficientResourcesAvailable))
			})
		})
	})

	Describe("Initialize", func() {
		var (
			req     *executor.RunRequest
			runInfo executor.RunInfo
			runTags executor.Tags
		)

		BeforeEach(func() {
			runInfo = executor.RunInfo{
				CPUWeight:    2,
				StartTimeout: 50,
				Privileged:   true,
			}

			runTags = executor.Tags{
				"Beep": "Boop",
			}

			req = &executor.RunRequest{
				Guid:    containerGuid,
				RunInfo: runInfo,
				Tags:    runTags,
			}
		})

		Context("when the container has not been reserved", func() {
			It("returns a container not found error", func() {
				err := containerStore.Initialize(logger, req)
				Expect(err).To(Equal(executor.ErrContainerNotFound))
			})
		})

		Context("when the conatiner is reserved", func() {
			BeforeEach(func() {
				allocationReq := &executor.AllocationRequest{
					Guid: containerGuid,
					Tags: executor.Tags{},
				}

				_, err := containerStore.Reserve(logger, allocationReq)
				Expect(err).NotTo(HaveOccurred())
			})

			It("populates the container with info from the run request", func() {
				err := containerStore.Initialize(logger, req)
				Expect(err).NotTo(HaveOccurred())

				container, err := containerStore.Get(logger, req.Guid)
				Expect(err).NotTo(HaveOccurred())
				Expect(container.State).To(Equal(executor.StateInitializing))
				Expect(container.RunInfo).To(Equal(runInfo))
				Expect(container.Tags).To(Equal(runTags))
			})
		})

		Context("when the container exists but is not reserved", func() {
			BeforeEach(func() {
				allocationReq := &executor.AllocationRequest{
					Guid: containerGuid,
					Tags: executor.Tags{},
				}

				_, err := containerStore.Reserve(logger, allocationReq)
				Expect(err).NotTo(HaveOccurred())

				err = containerStore.Initialize(logger, req)
				Expect(err).NotTo(HaveOccurred())
			})

			It("returns an invalid state tranistion error", func() {
				err := containerStore.Initialize(logger, req)
				Expect(err).To(Equal(executor.ErrInvalidTransition))
			})
		})
	})

	Describe("Create", func() {
		var (
			resource      executor.Resource
			allocationReq *executor.AllocationRequest
		)

		BeforeEach(func() {
			resource = executor.Resource{
				MemoryMB:   1024,
				DiskMB:     1024,
				RootFSPath: "/foo/bar",
			}

			allocationReq = &executor.AllocationRequest{
				Guid: containerGuid,
				Tags: executor.Tags{
					"Foo": "Bar",
				},
				Resource: resource,
			}
		})

		Context("when the container is initializing", func() {
			var (
				externalIP string
				runReq     *executor.RunRequest
			)

			BeforeEach(func() {
				externalIP = "6.6.6.6"
				env := []executor.EnvironmentVariable{
					{Name: "foo", Value: "bar"},
					{Name: "beep", Value: "booop"},
				}

				runInfo := executor.RunInfo{
					Privileged:   true,
					CPUWeight:    50,
					StartTimeout: 99,
					LogConfig: executor.LogConfig{
						Guid:       "log-guid",
						Index:      1,
						SourceName: "test-source",
					},
					MetricsConfig: executor.MetricsConfig{
						Guid:  "metric-guid",
						Index: 1,
					},
					Env: env,
				}
				runReq = &executor.RunRequest{
					Guid:    containerGuid,
					RunInfo: runInfo,
				}

				gardenContainer.InfoReturns(garden.ContainerInfo{ExternalIP: externalIP}, nil)
				gardenClient.CreateReturns(gardenContainer, nil)
			})

			JustBeforeEach(func() {
				_, err := containerStore.Reserve(logger, allocationReq)
				Expect(err).NotTo(HaveOccurred())

				err = containerStore.Initialize(logger, runReq)
				Expect(err).NotTo(HaveOccurred())
			})

			It("sets the container state to created", func() {
				_, err := containerStore.Create(logger, containerGuid)
				Expect(err).NotTo(HaveOccurred())

				container, err := containerStore.Get(logger, containerGuid)
				Expect(err).NotTo(HaveOccurred())
				Expect(container.State).To(Equal(executor.StateCreated))
			})

			It("creates the container in garden with the correct limits", func() {
				_, err := containerStore.Create(logger, containerGuid)
				Expect(err).NotTo(HaveOccurred())

				Expect(gardenClient.CreateCallCount()).To(Equal(1))
				containerSpec := gardenClient.CreateArgsForCall(0)
				Expect(containerSpec.Handle).To(Equal(containerGuid))
				Expect(containerSpec.RootFSPath).To(Equal(resource.RootFSPath))
				Expect(containerSpec.Privileged).To(Equal(true))

				Expect(containerSpec.Limits.Memory.LimitInBytes).To(BeEquivalentTo(resource.MemoryMB * 1024 * 1024))

				Expect(containerSpec.Limits.Disk.Scope).To(Equal(garden.DiskLimitScopeExclusive))
				Expect(containerSpec.Limits.Disk.ByteHard).To(BeEquivalentTo(resource.DiskMB * 1024 * 1024))
				Expect(containerSpec.Limits.Disk.InodeHard).To(Equal(iNodeLimit))

				expectedCPUShares := uint64(float64(maxCPUShares) * float64(runReq.CPUWeight) / 100.0)
				Expect(containerSpec.Limits.CPU.LimitInShares).To(Equal(expectedCPUShares))
			})

			It("creates the container with the correct properties", func() {
				_, err := containerStore.Create(logger, containerGuid)
				Expect(err).NotTo(HaveOccurred())

				Expect(gardenClient.CreateCallCount()).To(Equal(1))
				containerSpec := gardenClient.CreateArgsForCall(0)

				Expect(containerSpec.Properties).To(Equal(garden.Properties{
					containerstore.ContainerOwnerProperty: ownerName,
				}))
			})

			It("creates the container with the correct environment", func() {
				_, err := containerStore.Create(logger, containerGuid)
				Expect(err).NotTo(HaveOccurred())

				Expect(gardenClient.CreateCallCount()).To(Equal(1))
				containerSpec := gardenClient.CreateArgsForCall(0)

				expectedEnv := []string{}
				for _, envVar := range runReq.Env {
					expectedEnv = append(expectedEnv, envVar.Name+"="+envVar.Value)
				}
				Expect(containerSpec.Env).To(Equal(expectedEnv))
			})

			It("sets the correct external ip", func() {
				container, err := containerStore.Create(logger, containerGuid)
				Expect(err).NotTo(HaveOccurred())
				Expect(container.ExternalIP).To(Equal(externalIP))
			})

			Context("when egress rules are requested", func() {

				BeforeEach(func() {
					egressRules := []*models.SecurityGroupRule{
						{
							Protocol:     "icmp",
							Destinations: []string{"1.1.1.1"},
							IcmpInfo: &models.ICMPInfo{
								Type: 2,
								Code: 10,
							},
						},
						{
							Protocol:     "icmp",
							Destinations: []string{"1.1.1.1"},
							IcmpInfo: &models.ICMPInfo{
								Type: 2,
								Code: 10,
							},
						},
					}
					runReq.EgressRules = egressRules
				})

				It("calls NetOut for each egress rule", func() {
					_, err := containerStore.Create(logger, containerGuid)
					Expect(err).NotTo(HaveOccurred())

					Expect(gardenContainer.NetOutCallCount()).To(Equal(2))
				})

				Context("when NetOut fails", func() {
					BeforeEach(func() {
						gardenContainer.NetOutStub = func(garden.NetOutRule) error {
							if gardenContainer.NetOutCallCount() == 1 {
								return nil
							} else {
								return errors.New("failed net out!")
							}
						}
					})

					It("destroys the created container and returns an error", func() {
						_, err := containerStore.Create(logger, containerGuid)
						Expect(err).To(Equal(errors.New("failed net out!")))

						Expect(gardenClient.CreateCallCount()).To(Equal(1))
						Expect(gardenClient.DestroyCallCount()).To(Equal(1))

						Expect(gardenClient.DestroyArgsForCall(0)).To(Equal(containerGuid))
					})
				})

				Context("when a egress rule is not valid", func() {
					BeforeEach(func() {
						egressRule := &models.SecurityGroupRule{
							Protocol: "tcp",
						}
						runReq.EgressRules = append(runReq.EgressRules, egressRule)
					})

					It("returns an error", func() {
						_, err := containerStore.Create(logger, containerGuid)
						Expect(err).To(HaveOccurred())

						Expect(gardenClient.CreateCallCount()).To(Equal(0))
					})
				})
			})

			Context("when ports are requested", func() {
				BeforeEach(func() {
					portMapping := []executor.PortMapping{
						{ContainerPort: 8080},
						{ContainerPort: 9090},
					}
					runReq.Ports = portMapping

					gardenContainer.NetInStub = func(uint32, containerPort uint32) (uint32, uint32, error) {
						switch containerPort {
						case 8080:
							return 16000, 8080, nil
						case 9090:
							return 32000, 9090, nil
						default:
							return 0, 0, errors.New("failed-net-in")
						}
					}
				})

				It("calls NetIn on the container for each port", func() {
					_, err := containerStore.Create(logger, containerGuid)
					Expect(err).NotTo(HaveOccurred())

					Expect(gardenContainer.NetInCallCount()).To(Equal(2))
				})

				It("saves the actual port mappings on the container", func() {
					container, err := containerStore.Create(logger, containerGuid)
					Expect(err).NotTo(HaveOccurred())

					Expect(container.Ports[0].ContainerPort).To(BeEquivalentTo(8080))
					Expect(container.Ports[0].HostPort).To(BeEquivalentTo(16000))
					Expect(container.Ports[1].ContainerPort).To(BeEquivalentTo(9090))
					Expect(container.Ports[1].HostPort).To(BeEquivalentTo(32000))

					fetchedContainer, err := containerStore.Get(logger, containerGuid)
					Expect(err).NotTo(HaveOccurred())
					Expect(fetchedContainer).To(Equal(container))
				})

				Context("when NetIn fails", func() {
					BeforeEach(func() {
						gardenContainer.NetInReturns(0, 0, errors.New("failed generating net in rules"))
					})

					It("destroys the container and returns an error", func() {
						_, err := containerStore.Create(logger, containerGuid)
						Expect(err).To(HaveOccurred())

						Expect(gardenClient.DestroyCallCount()).To(Equal(1))
						Expect(gardenClient.DestroyArgsForCall(0)).To(Equal(containerGuid))
					})
				})
			})

			Context("when a total disk scope is request", func() {
				BeforeEach(func() {
					runReq.DiskScope = executor.TotalDiskLimit
				})

				It("creates the container with the correct disk scope", func() {
					_, err := containerStore.Create(logger, containerGuid)
					Expect(err).NotTo(HaveOccurred())

					Expect(gardenClient.CreateCallCount()).To(Equal(1))
					containerSpec := gardenClient.CreateArgsForCall(0)
					Expect(containerSpec.Limits.Disk.Scope).To(Equal(garden.DiskLimitScopeTotal))
				})
			})

			Context("when creating the container fails", func() {
				BeforeEach(func() {
					gardenClient.CreateReturns(nil, errors.New("boom!"))
				})

				It("returns an error", func() {
					_, err := containerStore.Create(logger, containerGuid)
					Expect(err).To(Equal(errors.New("boom!")))
				})
			})

			Context("when requesting the external IP for the created fails", func() {
				BeforeEach(func() {
					gardenContainer.InfoReturns(garden.ContainerInfo{}, errors.New("could not obtain info"))
				})

				It("returns an error", func() {
					_, err := containerStore.Create(logger, containerGuid)
					Expect(err).To(HaveOccurred())

					Expect(gardenClient.DestroyCallCount()).To(Equal(1))
					Expect(gardenClient.DestroyArgsForCall(0)).To(Equal(containerGuid))
				})
			})

			Context("when comparing and swapping fails", func() {
				var (
					finishInfo, infoCalled chan struct{}
					createErrCh            chan error
					done                   chan struct{}
				)

				BeforeEach(func() {
					finishInfo = make(chan struct{})
					infoCalled = make(chan struct{})
					createErrCh = make(chan error)
					done = make(chan struct{})

					gardenContainer.InfoStub = func() (garden.ContainerInfo, error) {
						close(infoCalled)
						<-finishInfo
						return garden.ContainerInfo{}, nil
					}
				})

				JustBeforeEach(func() {
					go func() {
						_, err := containerStore.Create(logger, containerGuid)
						createErrCh <- err
					}()
					Eventually(infoCalled).Should(BeClosed())
					_, err := containerStore.Fail(logger, containerGuid, "i failed.")
					Expect(err).NotTo(HaveOccurred())
					close(finishInfo)

					Eventually(createErrCh).Should(Receive(Equal(containerstore.ErrFailedToCAS)))
				})

				AfterEach(func() {
					close(done)
				})

				It("deletes the garden container", func() {
					Eventually(gardenClient.DestroyCallCount).Should(Equal(1))
				})

				It("does not update the container in the container store", func() {
					container, err := containerStore.Get(logger, containerGuid)
					Expect(err).NotTo(HaveOccurred())

					Expect(container.State).To(Equal(executor.StateCompleted))
					Expect(container.RunResult.Failed).To(Equal(true))
					Expect(container.RunResult.FailureReason).To(Equal("i failed."))
				})
			})
		})

		Context("when the container does not exist", func() {
			It("returns a conatiner not found error", func() {
				_, err := containerStore.Create(logger, "bogus-guid")
				Expect(err).To(Equal(executor.ErrContainerNotFound))
			})
		})

		Context("when the container is not initializing", func() {
			BeforeEach(func() {
				_, err := containerStore.Reserve(logger, allocationReq)
				Expect(err).NotTo(HaveOccurred())
			})

			It("returns an invalid state transition error", func() {
				_, err := containerStore.Create(logger, containerGuid)
				Expect(err).To(Equal(executor.ErrInvalidTransition))
			})
		})
	})

	Describe("Run", func() {
		var (
			allocationReq *executor.AllocationRequest
			runProcess    *gfakes.FakeProcess
		)

		BeforeEach(func() {
			allocationReq = &executor.AllocationRequest{
				Guid: containerGuid,
			}

			runProcess = &gfakes.FakeProcess{}

			gardenContainer.RunReturns(runProcess, nil)
			gardenClient.CreateReturns(gardenContainer, nil)
			gardenClient.LookupReturns(gardenContainer, nil)
		})

		Context("when it is in the created state", func() {
			var (
				runReq            *executor.RunRequest
				actionStep        *stepfakes.FakeStep
				healthCheckPassed chan struct{}
				finishPerforming  chan struct{}
				done              chan struct{}
				result            error
			)

			BeforeEach(func() {
				runAction := &models.Action{
					RunAction: &models.RunAction{
						Path: "/foo/bar",
					},
				}

				runReq = &executor.RunRequest{
					Guid: containerGuid,
					RunInfo: executor.RunInfo{
						Action: runAction,
					},
				}

				actionStep = &stepfakes.FakeStep{}
				healthCheckPassed = make(chan struct{})

				megatron.StepsForContainerReturns(actionStep, healthCheckPassed, nil)

				finishPerforming = make(chan struct{}, 1)
				done = make(chan struct{})
			})

			JustBeforeEach(func() {
				errResult := result
				actionStep.PerformStub = func() error {
					<-finishPerforming
					close(done)
					return errResult
				}

				_, err := containerStore.Reserve(logger, allocationReq)
				Expect(err).NotTo(HaveOccurred())

				err = containerStore.Initialize(logger, runReq)
				Expect(err).NotTo(HaveOccurred())

				_, err = containerStore.Create(logger, containerGuid)
				Expect(err).NotTo(HaveOccurred())
			})

			AfterEach(func() {
				close(finishPerforming)
				Eventually(done).Should(BeClosed())
			})

			It("performs the step", func() {
				err := containerStore.Run(logger, containerGuid)
				Expect(err).NotTo(HaveOccurred())

				Expect(megatron.StepsForContainerCallCount()).To(Equal(1))
				Eventually(actionStep.PerformCallCount).Should(Equal(1))
			})

			It("sets the container state to running once the healthcheck passes", func() {
				err := containerStore.Run(logger, containerGuid)
				Expect(err).NotTo(HaveOccurred())

				container, err := containerStore.Get(logger, containerGuid)
				Expect(err).NotTo(HaveOccurred())
				Expect(container.State).To(Equal(executor.StateCreated))

				healthCheckPassed <- struct{}{}
				Eventually(func() executor.State {
					container, err := containerStore.Get(logger, containerGuid)
					Expect(err).NotTo(HaveOccurred())
					return container.State
				}).Should(Equal(executor.StateRunning))
			})

			It("emits a container running event after the health check passes", func() {
				err := containerStore.Run(logger, containerGuid)
				Expect(err).NotTo(HaveOccurred())

				healthCheckPassed <- struct{}{}

				container, err := containerStore.Get(logger, containerGuid)
				Expect(err).NotTo(HaveOccurred())

				Eventually(eventEmitter.EmitCallCount).Should(Equal(2))
				event := eventEmitter.EmitArgsForCall(1)
				Expect(event).To(Equal(executor.ContainerRunningEvent{RawContainer: container}))
			})

			Context("when the action exits", func() {
				BeforeEach(func() {
					finishPerforming <- struct{}{}
				})

				It("sets its state to completed", func() {
					err := containerStore.Run(logger, containerGuid)
					Expect(err).NotTo(HaveOccurred())

					Eventually(actionStep.PerformCallCount).Should(Equal(1))

					container, err := containerStore.Get(logger, containerGuid)
					Expect(err).NotTo(HaveOccurred())
					Expect(container.State).To(Equal(executor.StateCompleted))
				})

				It("emits a container completed event", func() {
					err := containerStore.Run(logger, containerGuid)
					Expect(err).NotTo(HaveOccurred())

					Eventually(eventEmitter.EmitCallCount).Should(Equal(2))

					container, err := containerStore.Get(logger, containerGuid)
					Expect(err).NotTo(HaveOccurred())

					event := eventEmitter.EmitArgsForCall(1)
					Expect(event).To(Equal(executor.ContainerCompleteEvent{
						RawContainer: container,
					}))
				})

				Context("successfully", func() {
					It("sets the result on the container", func() {
						err := containerStore.Run(logger, containerGuid)
						Expect(err).NotTo(HaveOccurred())

						Eventually(actionStep.PerformCallCount).Should(Equal(1))

						container, err := containerStore.Get(logger, containerGuid)
						Expect(err).NotTo(HaveOccurred())
						Expect(container.RunResult.Failed).To(Equal(false))
						Expect(container.RunResult.Stopped).To(Equal(false))
					})
				})

				Context("unsuccessfully", func() {
					BeforeEach(func() {
						result = errors.New("BOOOOOOM!!!!!!!")
					})

					It("sets the run result on the container", func() {
						err := containerStore.Run(logger, containerGuid)
						Expect(err).NotTo(HaveOccurred())

						Eventually(actionStep.PerformCallCount).Should(Equal(1))

						container, err := containerStore.Get(logger, containerGuid)
						Expect(err).NotTo(HaveOccurred())
						Expect(container.RunResult.Failed).To(Equal(true))
						Expect(container.RunResult.FailureReason).To(Equal("BOOOOOOM!!!!!!!"))
						Expect(container.RunResult.Stopped).To(Equal(false))
					})
				})
			})

			Context("when the transformer fails to generate steps", func() {
				BeforeEach(func() {
					megatron.StepsForContainerReturns(nil, nil, errors.New("defeated by the auto bots"))
				})

				It("returns an error", func() {
					err := containerStore.Run(logger, containerGuid)
					Expect(err).To(HaveOccurred())
					close(done)
				})
			})
		})

		Context("when the container does not exist", func() {
			It("returns an ErrContainerNotFound error", func() {
				err := containerStore.Run(logger, containerGuid)
				Expect(err).To(Equal(executor.ErrContainerNotFound))
			})
		})

		Context("When the container is not in the created state", func() {
			JustBeforeEach(func() {
				_, err := containerStore.Reserve(logger, allocationReq)
				Expect(err).NotTo(HaveOccurred())
			})

			It("returns a transition error", func() {
				err := containerStore.Run(logger, containerGuid)
				Expect(err).To(Equal(executor.ErrInvalidTransition))
			})
		})
	})

	Describe("Fail", func() {
		var containerFailureReason string

		BeforeEach(func() {
			containerFailureReason = "error creating container"

		})

		JustBeforeEach(func() {
			_, err := containerStore.Reserve(logger, &executor.AllocationRequest{Guid: containerGuid})
			Expect(err).NotTo(HaveOccurred())
		})

		It("sets the garden container state to completed", func() {
			container, err := containerStore.Fail(logger, containerGuid, containerFailureReason)
			Expect(err).NotTo(HaveOccurred())
			Expect(container.State).To(Equal(executor.StateCompleted))

			fetchedContainer, err := containerStore.Get(logger, containerGuid)
			Expect(err).NotTo(HaveOccurred())
			Expect(fetchedContainer).To(Equal(container))
		})

		It("sets the container result", func() {
			container, err := containerStore.Fail(logger, containerGuid, containerFailureReason)
			Expect(err).NotTo(HaveOccurred())
			Expect(container.RunResult.Failed).To(BeTrue())
			Expect(container.RunResult.FailureReason).To(Equal(containerFailureReason))

			fetchedContainer, err := containerStore.Get(logger, containerGuid)
			Expect(err).NotTo(HaveOccurred())
			Expect(fetchedContainer).To(Equal(container))
		})

		It("emits a container complete event", func() {
			container, err := containerStore.Fail(logger, containerGuid, containerFailureReason)
			Expect(err).NotTo(HaveOccurred())

			Eventually(eventEmitter.EmitCallCount).Should(Equal(2))
			event := eventEmitter.EmitArgsForCall(1)
			Expect(event).To(Equal(executor.ContainerCompleteEvent{RawContainer: container}))
		})

		Context("when the container is already completed", func() {
			JustBeforeEach(func() {
				_, err := containerStore.Fail(logger, containerGuid, containerFailureReason)
				Expect(err).NotTo(HaveOccurred())
			})

			It("returns an ErrInvalidTransition", func() {
				_, err := containerStore.Fail(logger, containerGuid, containerFailureReason)
				Expect(err).To(Equal(executor.ErrInvalidTransition))
			})
		})

		Context("when the container does not exist", func() {
			It("returns a ErrContainerNotFound", func() {
				_, err := containerStore.Fail(logger, "", containerFailureReason)
				Expect(err).To(Equal(executor.ErrContainerNotFound))
			})
		})
	})

	Describe("Stop", func() {
		var (
			actionStep *stepfakes.FakeStep
		)

		BeforeEach(func() {
			actionStep = &stepfakes.FakeStep{}

			gardenClient.CreateReturns(gardenContainer, nil)
			megatron.StepsForContainerReturns(actionStep, nil, nil)
		})

		JustBeforeEach(func() {
			_, err := containerStore.Reserve(logger, &executor.AllocationRequest{Guid: containerGuid})
			Expect(err).NotTo(HaveOccurred())

			err = containerStore.Initialize(logger, &executor.RunRequest{Guid: containerGuid})
			Expect(err).NotTo(HaveOccurred())

			_, err = containerStore.Create(logger, containerGuid)
			Expect(err).NotTo(HaveOccurred())
		})

		Context("when the container has processes associated with it", func() {
			var finishRun chan struct{}
			BeforeEach(func() {
				finishRun = make(chan struct{})
				actionStep.PerformStub = func() error {
					<-finishRun
					return nil
				}
			})

			JustBeforeEach(func() {
				err := containerStore.Run(logger, containerGuid)
				Expect(err).NotTo(HaveOccurred())
			})

			It("calls cancel and waits for the running process to return", func() {
				errCh := make(chan error)

				go func() {
					errCh <- containerStore.Stop(logger, containerGuid)
				}()

				Eventually(actionStep.CancelCallCount).Should(Equal(1))
				Consistently(errCh).ShouldNot(Receive())
				close(finishRun)
				Eventually(errCh).Should(Receive())
			})

			It("sets stopped to true on the run result", func() {
				errCh := make(chan error)
				go func() {
					errCh <- containerStore.Stop(logger, containerGuid)
				}()
				Eventually(actionStep.CancelCallCount).Should(Equal(1))
				close(finishRun)
				Eventually(errCh).Should(Receive())

				container, err := containerStore.Get(logger, containerGuid)
				Expect(err).NotTo(HaveOccurred())
				Expect(container.RunResult.Stopped).To(BeTrue())
			})

			It("removes the runningProcess", func() {
				close(finishRun)
				err := containerStore.Stop(logger, containerGuid)
				Expect(err).NotTo(HaveOccurred())

				err = containerStore.Stop(logger, containerGuid)
				Expect(err).To(Equal(executor.ErrNoProcessToStop))
			})
		})

		Context("when the container does not have processes associated with it", func() {
			It("returns a ErrFailedToStop", func() {
				err := containerStore.Stop(logger, containerGuid)
				Expect(err).To(Equal(executor.ErrNoProcessToStop))
			})
		})

		Context("when the container does not exist", func() {
			It("returns an ErrContainerNotFound", func() {
				err := containerStore.Stop(logger, "")
				Expect(err).To(Equal(executor.ErrContainerNotFound))
			})
		})
	})

	Describe("Destroy", func() {
		var resource executor.Resource

		BeforeEach(func() {
			gardenClient.CreateReturns(gardenContainer, nil)
			resource = executor.NewResource(1024, 2048, "foobar")
		})

		JustBeforeEach(func() {
			_, err := containerStore.Reserve(logger, &executor.AllocationRequest{Guid: containerGuid, Resource: resource})
			Expect(err).NotTo(HaveOccurred())

			err = containerStore.Initialize(logger, &executor.RunRequest{Guid: containerGuid})
			Expect(err).NotTo(HaveOccurred())

			_, err = containerStore.Create(logger, containerGuid)
			Expect(err).NotTo(HaveOccurred())
		})

		It("destroys the container", func() {
			err := containerStore.Destroy(logger, containerGuid)
			Expect(err).NotTo(HaveOccurred())

			Expect(gardenClient.DestroyCallCount()).To(Equal(1))
			Expect(gardenClient.DestroyArgsForCall(0)).To(Equal(containerGuid))

			_, err = containerStore.Get(logger, containerGuid)
			Expect(err).To(Equal(executor.ErrContainerNotFound))
		})

		It("frees the containers resources", func() {
			err := containerStore.Destroy(logger, containerGuid)
			Expect(err).NotTo(HaveOccurred())

			remainingResources := containerStore.RemainingResources(logger)
			Expect(remainingResources).To(Equal(totalCapacity))
		})

		Context("when destroying the garden container fails", func() {
			var destroyErr error
			BeforeEach(func() {
				destroyErr = errors.New("failed to destroy garden container")
			})

			JustBeforeEach(func() {
				gardenClient.DestroyReturns(destroyErr)
			})

			Context("because the garden container does not exist", func() {
				BeforeEach(func() {
					destroyErr = garden.ContainerNotFoundError{}
				})

				It("does not return an error", func() {
					err := containerStore.Destroy(logger, containerGuid)
					Expect(err).NotTo(HaveOccurred())
				})
			})

			Context("for unknown reason", func() {
				It("returns an error", func() {
					err := containerStore.Destroy(logger, containerGuid)
					Expect(err).To(Equal(destroyErr))
				})

				It("does not remove the container from the container store", func() {
					err := containerStore.Destroy(logger, containerGuid)
					Expect(err).To(HaveOccurred())

					container, err := containerStore.Get(logger, containerGuid)
					Expect(err).NotTo(HaveOccurred())
					Expect(container.Guid).To(Equal(containerGuid))
				})

				It("does not return the resources", func() {
					err := containerStore.Destroy(logger, containerGuid)
					Expect(err).To(HaveOccurred())

					resources := containerStore.RemainingResources(logger)
					expectedResources := totalCapacity.Copy()
					expectedResources.Subtract(&resource)
					Expect(resources).To(Equal(expectedResources))
				})
			})
		})

		Context("when the container does not exist", func() {
			It("returns a ErrContainerNotFound", func() {
				err := containerStore.Destroy(logger, "")
				Expect(err).To(Equal(executor.ErrContainerNotFound))
			})
		})

		Context("when there is a running process associated with the container", func() {
			var (
				finishRun  chan struct{}
				actionStep *stepfakes.FakeStep
			)

			BeforeEach(func() {
				actionStep = &stepfakes.FakeStep{}

				megatron.StepsForContainerReturns(actionStep, nil, nil)

				finishRun = make(chan struct{})
				actionStep.PerformStub = func() error {
					<-finishRun
					return nil
				}
			})

			JustBeforeEach(func() {
				err := containerStore.Run(logger, containerGuid)
				Expect(err).NotTo(HaveOccurred())
			})

			It("cancels the process", func() {
				errCh := make(chan error)
				go func() {
					errCh <- containerStore.Destroy(logger, containerGuid)
				}()

				Eventually(actionStep.CancelCallCount).Should(Equal(1))
				Consistently(errCh).ShouldNot(Receive())
				close(finishRun)
				Eventually(errCh).Should(Receive())
			})
		})
	})

	Describe("Get", func() {
		BeforeEach(func() {
			_, err := containerStore.Reserve(logger, &executor.AllocationRequest{Guid: containerGuid})
			Expect(err).NotTo(HaveOccurred())
		})

		It("returns the specified container", func() {
			container, err := containerStore.Get(logger, containerGuid)
			Expect(err).NotTo(HaveOccurred())

			Expect(container.Guid).To(Equal(containerGuid))
		})

		Context("when the container does not exist", func() {
			It("returns an ErrContainerNotFound", func() {
				_, err := containerStore.Get(logger, "")
				Expect(err).To(Equal(executor.ErrContainerNotFound))
			})
		})
	})

	Describe("List", func() {
		var container1, container2 executor.Container

		BeforeEach(func() {
			_, err := containerStore.Reserve(logger, &executor.AllocationRequest{
				Guid: containerGuid,
			})
			Expect(err).NotTo(HaveOccurred())

			err = containerStore.Initialize(logger, &executor.RunRequest{
				Guid: containerGuid,
			})
			Expect(err).NotTo(HaveOccurred())

			_, err = containerStore.Reserve(logger, &executor.AllocationRequest{
				Guid: containerGuid + "2",
			})
			Expect(err).NotTo(HaveOccurred())

			container1, err = containerStore.Get(logger, containerGuid)
			Expect(err).NotTo(HaveOccurred())

			container2, err = containerStore.Get(logger, containerGuid+"2")
			Expect(err).NotTo(HaveOccurred())
		})

		It("returns the list of known containers", func() {
			containers := containerStore.List(logger)
			Expect(containers).To(HaveLen(2))
			Expect(containers).To(ContainElement(container1))
			Expect(containers).To(ContainElement(container2))
		})
	})

	Describe("Metrics", func() {
		var containerGuid1, containerGuid2, containerGuid3, containerGuid4 string

		BeforeEach(func() {
			containerGuid1 = "container-guid-1"
			containerGuid2 = "container-guid-2"
			containerGuid3 = "container-guid-3"
			containerGuid4 = "container-guid-4"

			_, err := containerStore.Reserve(logger, &executor.AllocationRequest{Guid: containerGuid1})
			Expect(err).NotTo(HaveOccurred())

			_, err = containerStore.Reserve(logger, &executor.AllocationRequest{Guid: containerGuid2})
			Expect(err).NotTo(HaveOccurred())

			_, err = containerStore.Reserve(logger, &executor.AllocationRequest{Guid: containerGuid3})
			Expect(err).NotTo(HaveOccurred())

			_, err = containerStore.Reserve(logger, &executor.AllocationRequest{Guid: containerGuid4})
			Expect(err).NotTo(HaveOccurred())

			bulkMetrics := map[string]garden.ContainerMetricsEntry{
				containerGuid1: garden.ContainerMetricsEntry{
					Metrics: garden.Metrics{
						MemoryStat: garden.ContainerMemoryStat{
							TotalUsageTowardLimit: 1024,
						},
						DiskStat: garden.ContainerDiskStat{
							ExclusiveBytesUsed: 2048,
						},
						CPUStat: garden.ContainerCPUStat{
							Usage: 5000000000,
						},
					},
				},
				containerGuid2: garden.ContainerMetricsEntry{
					Metrics: garden.Metrics{
						MemoryStat: garden.ContainerMemoryStat{
							TotalUsageTowardLimit: 512,
						},
						DiskStat: garden.ContainerDiskStat{
							ExclusiveBytesUsed: 128,
						},
						CPUStat: garden.ContainerCPUStat{
							Usage: 1000000,
						},
					},
				},
				containerGuid4: garden.ContainerMetricsEntry{
					Err: &garden.Error{Err: errors.New("no-metrics-here")},
					Metrics: garden.Metrics{
						MemoryStat: garden.ContainerMemoryStat{
							TotalUsageTowardLimit: 512,
						},
						DiskStat: garden.ContainerDiskStat{
							ExclusiveBytesUsed: 128,
						},
						CPUStat: garden.ContainerCPUStat{
							Usage: 1000000,
						},
					},
				},
				"BOGUS-GUID": garden.ContainerMetricsEntry{},
			}

			gardenClient.BulkMetricsReturns(bulkMetrics, nil)
		})

		It("returns metrics for all known containers", func() {
			metrics, err := containerStore.Metrics(logger)
			Expect(err).NotTo(HaveOccurred())

			Expect(gardenClient.BulkMetricsCallCount()).To(Equal(1))
			Expect(gardenClient.BulkMetricsArgsForCall(0)).To(ConsistOf(
				containerGuid1, containerGuid2, containerGuid3, containerGuid4,
			))

			Expect(metrics).To(HaveLen(2))

			container1Metrics, ok := metrics[containerGuid1]
			Expect(ok).To(BeTrue())
			Expect(container1Metrics.MemoryUsageInBytes).To(BeEquivalentTo(1024))
			Expect(container1Metrics.DiskUsageInBytes).To(BeEquivalentTo(2048))
			Expect(container1Metrics.TimeSpentInCPU).To(Equal(5 * time.Second))

			container2Metrics, ok := metrics[containerGuid2]
			Expect(ok).To(BeTrue())
			Expect(container2Metrics.MemoryUsageInBytes).To(BeEquivalentTo(512))
			Expect(container2Metrics.DiskUsageInBytes).To(BeEquivalentTo(128))
			Expect(container2Metrics.TimeSpentInCPU).To(Equal(1 * time.Millisecond))
		})

		Context("when fetching bulk metrics fails", func() {
			BeforeEach(func() {
				gardenClient.BulkMetricsReturns(nil, errors.New("failed-bulk-metrics"))
			})

			It("returns an error", func() {
				_, err := containerStore.Metrics(logger)
				Expect(err).To(Equal(errors.New("failed-bulk-metrics")))
			})
		})
	})

	Describe("GetFiles", func() {
		BeforeEach(func() {
			gardenClient.CreateReturns(gardenContainer, nil)
			gardenContainer.StreamOutReturns(ioutil.NopCloser(bytes.NewReader([]byte("this is the stream"))), nil)
		})

		JustBeforeEach(func() {
			_, err := containerStore.Reserve(logger, &executor.AllocationRequest{Guid: containerGuid})
			Expect(err).NotTo(HaveOccurred())
		})

		Context("when the container has a corresponding garden container", func() {
			JustBeforeEach(func() {
				err := containerStore.Initialize(logger, &executor.RunRequest{Guid: containerGuid})
				Expect(err).NotTo(HaveOccurred())

				_, err = containerStore.Create(logger, containerGuid)
				Expect(err).NotTo(HaveOccurred())
			})

			It("calls streamout on the garden client", func() {
				stream, err := containerStore.GetFiles(logger, containerGuid, "/path/to/file")
				Expect(err).NotTo(HaveOccurred())

				Expect(gardenContainer.StreamOutCallCount()).To(Equal(1))
				streamOutSpec := gardenContainer.StreamOutArgsForCall(0)
				Expect(streamOutSpec.Path).To(Equal("/path/to/file"))
				Expect(streamOutSpec.User).To(Equal("root"))

				output := make([]byte, len("this is the stream"))
				_, err = stream.Read(output)
				Expect(err).NotTo(HaveOccurred())
				Expect(output).To(Equal([]byte("this is the stream")))
			})
		})

		Context("when the container does not have a corresponding garden container", func() {
			It("returns an error", func() {
				_, err := containerStore.GetFiles(logger, containerGuid, "/path")
				Expect(err).To(Equal(executor.ErrContainerNotFound))
			})
		})

		Context("when the container does not exist", func() {
			It("returns ErrContainerNotFound", func() {
				_, err := containerStore.GetFiles(logger, "", "/stuff")
				Expect(err).To(Equal(executor.ErrContainerNotFound))
			})
		})
	})

	Describe("RegistryPruner", func() {
		var (
			expirationTime time.Duration
			process        ifrit.Process
			resource       executor.Resource
		)

		BeforeEach(func() {
			resource = executor.NewResource(512, 512, "")
			req := executor.NewAllocationRequest("forever-reserved", &resource, nil)

			_, err := containerStore.Reserve(logger, &req)
			Expect(err).NotTo(HaveOccurred())

			resource = executor.NewResource(512, 512, "")
			req = executor.NewAllocationRequest("eventually-initialized", &resource, nil)

			_, err = containerStore.Reserve(logger, &req)
			Expect(err).NotTo(HaveOccurred())

			runReq := executor.NewRunRequest("eventually-initialized", &executor.RunInfo{}, executor.Tags{})
			err = containerStore.Initialize(logger, &runReq)
			Expect(err).NotTo(HaveOccurred())

			expirationTime = 20 * time.Millisecond

			pruner := containerStore.RegistryPruner(logger, expirationTime)
			process = ginkgomon.Invoke(pruner)
		})

		AfterEach(func() {
			ginkgomon.Interrupt(process)
		})

		Context("when the elapsed time is less than expiration period", func() {
			BeforeEach(func() {
				clock.Increment(expirationTime / 2)
			})

			It("still has all the containers in the list", func() {
				Consistently(func() []executor.Container {
					return containerStore.List(logger)
				}).Should(HaveLen(2))

				resources := containerStore.RemainingResources(logger)
				expectedResources := totalCapacity.Copy()
				expectedResources.Subtract(&resource)
				expectedResources.Subtract(&resource)
				Expect(resources).To(Equal(expectedResources))
			})
		})

		Context("when the elapsed time is more than expiration period", func() {
			BeforeEach(func() {
				clock.Increment(2 * expirationTime)
			})

			It("removes only RESERVED containers from the list", func() {
				Eventually(func() []executor.Container {
					return containerStore.List(logger)
				}).Should(HaveLen(1))
				Expect(containerStore.List(logger)[0].Guid).To(Equal("eventually-initialized"))

				resources := containerStore.RemainingResources(logger)
				expectedResources := totalCapacity.Copy()
				expectedResources.Subtract(&resource)
				Expect(resources).To(Equal(expectedResources))
			})
		})
	})

	Describe("ContainerReaper", func() {
		var (
			containerGuid1, containerGuid2, containerGuid3, containerGuid4, containerGuid5 string
			process                                                                        ifrit.Process
			extraGardenContainer                                                           *gfakes.FakeContainer
		)

		BeforeEach(func() {
			gardenClient.CreateReturns(gardenContainer, nil)

			containerGuid1 = "container-guid-1"
			containerGuid2 = "container-guid-2"
			containerGuid3 = "container-guid-3"
			containerGuid4 = "container-guid-4"
			containerGuid5 = "container-guid-5"

			// Reserve
			_, err := containerStore.Reserve(logger, &executor.AllocationRequest{Guid: containerGuid1})
			Expect(err).NotTo(HaveOccurred())
			_, err = containerStore.Reserve(logger, &executor.AllocationRequest{Guid: containerGuid2})
			Expect(err).NotTo(HaveOccurred())
			_, err = containerStore.Reserve(logger, &executor.AllocationRequest{Guid: containerGuid3})
			Expect(err).NotTo(HaveOccurred())
			_, err = containerStore.Reserve(logger, &executor.AllocationRequest{Guid: containerGuid4})
			Expect(err).NotTo(HaveOccurred())
			_, err = containerStore.Reserve(logger, &executor.AllocationRequest{Guid: containerGuid5})
			Expect(err).NotTo(HaveOccurred())

			// Initialize
			err = containerStore.Initialize(logger, &executor.RunRequest{Guid: containerGuid2})
			Expect(err).NotTo(HaveOccurred())
			err = containerStore.Initialize(logger, &executor.RunRequest{Guid: containerGuid3})
			Expect(err).NotTo(HaveOccurred())
			err = containerStore.Initialize(logger, &executor.RunRequest{Guid: containerGuid4})
			Expect(err).NotTo(HaveOccurred())
			err = containerStore.Initialize(logger, &executor.RunRequest{Guid: containerGuid5})
			Expect(err).NotTo(HaveOccurred())

			// Create Containers
			_, err = containerStore.Create(logger, containerGuid3)
			Expect(err).NotTo(HaveOccurred())
			_, err = containerStore.Create(logger, containerGuid4)
			Expect(err).NotTo(HaveOccurred())
			_, err = containerStore.Create(logger, containerGuid5)
			Expect(err).NotTo(HaveOccurred())

			extraGardenContainer = &gfakes.FakeContainer{}
			extraGardenContainer.HandleReturns("foobar")
			gardenContainer.HandleReturns(containerGuid3)
			gardenContainers := []garden.Container{gardenContainer, extraGardenContainer}
			gardenClient.ContainersReturns(gardenContainers, nil)
		})

		JustBeforeEach(func() {
			reaper := containerStore.ContainerReaper(logger, 20*time.Millisecond)
			process = ginkgomon.Invoke(reaper)
		})

		AfterEach(func() {
			ginkgomon.Interrupt(process)
		})

		It("removes containers that no longer have corresponding garden containers", func() {
			Consistently(func() []executor.Container {
				return containerStore.List(logger)
			}).Should(HaveLen(5))

			clock.Increment(30 * time.Millisecond)

			Eventually(func() []executor.Container {
				return containerStore.List(logger)
			}).Should(HaveLen(3))

			Expect(gardenClient.ContainersCallCount()).To(Equal(2))

			properties := gardenClient.ContainersArgsForCall(0)
			Expect(properties[containerstore.ContainerOwnerProperty]).To(Equal(ownerName))
			properties = gardenClient.ContainersArgsForCall(1)
			Expect(properties[containerstore.ContainerOwnerProperty]).To(Equal(ownerName))

			Expect(gardenClient.DestroyCallCount()).To(Equal(1))
			Expect(gardenClient.DestroyArgsForCall(0)).To(Equal(extraGardenContainer.Handle()))

			clock.Increment(30 * time.Millisecond)
			Eventually(gardenClient.ContainersCallCount).Should(Equal(2))
		})

		Context("when listing containers in garden fails", func() {
			BeforeEach(func() {
				gardenClient.ContainersReturns([]garden.Container{}, errors.New("failed-to-list"))
			})

			It("logs the failure and continues", func() {
				clock.Increment(30 * time.Millisecond)
				Eventually(logger).Should(gbytes.Say("failed-to-fetch-containers"))

				Consistently(func() []executor.Container {
					return containerStore.List(logger)
				}).Should(HaveLen(5))
			})
		})

		Context("when destroying the extra container fails", func() {
			BeforeEach(func() {
				gardenClient.DestroyReturns(errors.New("failed-to-destroy"))
			})

			It("logs the error and continues", func() {
				clock.Increment(30 * time.Millisecond)
				Eventually(logger).Should(gbytes.Say("failed-to-destroy-container"))
			})
		})
	})
})
