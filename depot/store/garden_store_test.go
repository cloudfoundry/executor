package store_test

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"os"
	"strconv"
	"time"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/executor/depot/store"
	garden "github.com/cloudfoundry-incubator/garden/api"
	gfakes "github.com/cloudfoundry-incubator/garden/api/fakes"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/pivotal-golang/lager/lagertest"
	"github.com/pivotal-golang/timer/fake_timer"
	"github.com/tedsuo/ifrit"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
)

var _ = Describe("GardenContainerStore", func() {
	var (
		gardenStore      *store.GardenStore
		fakeGardenClient *gfakes.FakeClient

		ownerName           = "some-owner-name"
		inodeLimit   uint64 = 2000000
		maxCPUShares uint64 = 1024

		fakeTimer *fake_timer.FakeTimer
	)

	BeforeEach(func() {
		fakeTimer = fake_timer.NewFakeTimer(time.Now())

		fakeGardenClient = new(gfakes.FakeClient)
		gardenStore = store.NewGardenStore(
			lagertest.NewTestLogger("test"),
			fakeGardenClient,
			ownerName,
			maxCPUShares,
			inodeLimit,
			nil,
			nil,
			fakeTimer,
		)
	})

	Describe("Lookup", func() {
		var (
			executorContainer executor.Container
			lookupErr         error
		)

		JustBeforeEach(func() {
			executorContainer, lookupErr = gardenStore.Lookup("some-container-handle")
		})

		Context("when the container doesn't exist", func() {
			BeforeEach(func() {
				fakeGardenClient.LookupReturns(nil, errors.New("didn't find it"))
			})

			It("returns a container-not-found error", func() {
				Ω(lookupErr).Should(Equal(store.ErrContainerNotFound))
			})
		})

		Context("when the container exists", func() {
			var gardenContainer *gfakes.FakeContainer

			BeforeEach(func() {
				gardenContainer = new(gfakes.FakeContainer)
				gardenContainer.HandleReturns("some-container-handle")

				fakeGardenClient.LookupReturns(gardenContainer, nil)
			})

			It("does not error", func() {
				Ω(lookupErr).ShouldNot(HaveOccurred())
			})

			It("has the Garden container handle as its container guid", func() {
				Ω(executorContainer.Guid).Should(Equal("some-container-handle"))
			})

			It("looked up by the given guid", func() {
				Ω(fakeGardenClient.LookupArgsForCall(0)).Should(Equal("some-container-handle"))
			})

			Context("when the container has an executor:state property", func() {
				Context("and it's Reserved", func() {
					BeforeEach(func() {
						gardenContainer.InfoReturns(garden.ContainerInfo{
							Properties: garden.Properties{
								"executor:state": string(executor.StateReserved),
							},
						}, nil)
					})

					It("has it as its state", func() {
						Ω(executorContainer.State).Should(Equal(executor.StateReserved))
					})
				})

				Context("and it's Initializing", func() {
					BeforeEach(func() {
						gardenContainer.InfoReturns(garden.ContainerInfo{
							Properties: garden.Properties{
								"executor:state": string(executor.StateInitializing),
							},
						}, nil)
					})

					It("has it as its state", func() {
						Ω(executorContainer.State).Should(Equal(executor.StateInitializing))
					})
				})

				Context("and it's Created", func() {
					BeforeEach(func() {
						gardenContainer.InfoReturns(garden.ContainerInfo{
							Properties: garden.Properties{
								"executor:state": string(executor.StateCreated),
							},
						}, nil)
					})

					It("has it as its state", func() {
						Ω(executorContainer.State).Should(Equal(executor.StateCreated))
					})
				})

				Context("and it's Completed", func() {
					BeforeEach(func() {
						gardenContainer.InfoReturns(garden.ContainerInfo{
							Properties: garden.Properties{
								"executor:state": string(executor.StateCompleted),
							},
						}, nil)
					})

					It("has it as its state", func() {
						Ω(executorContainer.State).Should(Equal(executor.StateCompleted))
					})
				})

				Context("when it's some other state", func() {
					BeforeEach(func() {
						gardenContainer.InfoReturns(garden.ContainerInfo{
							Properties: garden.Properties{
								"executor:state": "bogus-state",
							},
						}, nil)
					})

					It("returns an InvalidStateError", func() {
						Ω(lookupErr).Should(Equal(store.InvalidStateError{"bogus-state"}))
					})
				})
			})

			Context("when the container has an executor:allocated-at property", func() {
				Context("when it's a valid integer", func() {
					BeforeEach(func() {
						gardenContainer.InfoReturns(garden.ContainerInfo{
							Properties: garden.Properties{
								"executor:allocated-at": "123",
							},
						}, nil)
					})

					It("has it as its allocated at value", func() {
						Ω(executorContainer.AllocatedAt).Should(Equal(int64(123)))
					})
				})

				Context("when it's a bogus value", func() {
					BeforeEach(func() {
						gardenContainer.InfoReturns(garden.ContainerInfo{
							Properties: garden.Properties{
								"executor:allocated-at": "some-bogus-timestamp",
							},
						}, nil)
					})

					It("returns a MalformedPropertyError", func() {
						Ω(lookupErr).Should(Equal(store.MalformedPropertyError{
							Property: "executor:allocated-at",
							Value:    "some-bogus-timestamp",
						}))
					})
				})
			})

			Context("when the container has an executor:memory-mb property", func() {
				Context("when it's a valid integer", func() {
					BeforeEach(func() {
						gardenContainer.InfoReturns(garden.ContainerInfo{
							Properties: garden.Properties{
								"executor:memory-mb": "1024",
							},
						}, nil)
					})

					It("has it as its rootfs path", func() {
						Ω(executorContainer.MemoryMB).Should(Equal(1024))
					})
				})

				Context("when it's a bogus value", func() {
					BeforeEach(func() {
						gardenContainer.InfoReturns(garden.ContainerInfo{
							Properties: garden.Properties{
								"executor:memory-mb": "some-bogus-integer",
							},
						}, nil)
					})

					It("returns a MalformedPropertyError", func() {
						Ω(lookupErr).Should(Equal(store.MalformedPropertyError{
							Property: "executor:memory-mb",
							Value:    "some-bogus-integer",
						}))
					})
				})
			})

			Context("when the container has an executor:disk-mb property", func() {
				Context("when it's a valid integer", func() {
					BeforeEach(func() {
						gardenContainer.InfoReturns(garden.ContainerInfo{
							Properties: garden.Properties{
								"executor:disk-mb": "2048",
							},
						}, nil)
					})

					It("has it as its disk reservation", func() {
						Ω(executorContainer.DiskMB).Should(Equal(2048))
					})
				})

				Context("when it's a bogus value", func() {
					BeforeEach(func() {
						gardenContainer.InfoReturns(garden.ContainerInfo{
							Properties: garden.Properties{
								"executor:disk-mb": "some-bogus-integer",
							},
						}, nil)
					})

					It("returns a MalformedPropertyError", func() {
						Ω(lookupErr).Should(Equal(store.MalformedPropertyError{
							Property: "executor:disk-mb",
							Value:    "some-bogus-integer",
						}))
					})
				})
			})

			Context("when the container has an executor:complete-url property", func() {
				BeforeEach(func() {
					gardenContainer.InfoReturns(garden.ContainerInfo{
						Properties: garden.Properties{
							"executor:complete-url": "gopher://google.com",
						},
					}, nil)
				})

				It("has it as its rootfs path", func() {
					Ω(executorContainer.CompleteURL).Should(Equal("gopher://google.com"))
				})
			})

			Context("when the container has an executor:actions property", func() {
				Context("and the actions are valid", func() {
					actions := []models.ExecutorAction{
						{
							models.RunAction{
								Path: "ls",
							},
						},
					}

					BeforeEach(func() {
						payload, err := json.Marshal(actions)
						Ω(err).ShouldNot(HaveOccurred())

						gardenContainer.InfoReturns(garden.ContainerInfo{
							Properties: garden.Properties{
								"executor:actions": string(payload),
							},
						}, nil)
					})

					It("has it as its action", func() {
						Ω(executorContainer.Actions).Should(Equal(actions))
					})
				})

				Context("and the actions are invalid", func() {
					BeforeEach(func() {
						gardenContainer.InfoReturns(garden.ContainerInfo{
							Properties: garden.Properties{
								"executor:actions": "ß",
							},
						}, nil)
					})

					It("returns an InvalidJSONError", func() {
						Ω(lookupErr).Should(HaveOccurred())
						Ω(lookupErr.Error()).Should(ContainSubstring("executor:actions"))
						Ω(lookupErr.Error()).Should(ContainSubstring("ß"))
						Ω(lookupErr.Error()).Should(ContainSubstring("invalid character"))
					})
				})
			})

			Context("when the container has an executor:env property", func() {
				Context("and the env is valid", func() {
					env := []executor.EnvironmentVariable{
						{Name: "FOO", Value: "bar"},
					}

					BeforeEach(func() {
						payload, err := json.Marshal(env)
						Ω(err).ShouldNot(HaveOccurred())

						gardenContainer.InfoReturns(garden.ContainerInfo{
							Properties: garden.Properties{
								"executor:env": string(payload),
							},
						}, nil)
					})

					It("has it as its env", func() {
						Ω(executorContainer.Env).Should(Equal(env))
					})
				})

				Context("and the env is invalid", func() {
					BeforeEach(func() {
						gardenContainer.InfoReturns(garden.ContainerInfo{
							Properties: garden.Properties{
								"executor:env": "ß",
							},
						}, nil)
					})

					It("returns an InvalidJSONError", func() {
						Ω(lookupErr).Should(HaveOccurred())
						Ω(lookupErr.Error()).Should(ContainSubstring("executor:env"))
						Ω(lookupErr.Error()).Should(ContainSubstring("ß"))
						Ω(lookupErr.Error()).Should(ContainSubstring("invalid character"))
					})
				})
			})

			Context("when the Garden container has tags", func() {
				BeforeEach(func() {
					gardenContainer.InfoReturns(garden.ContainerInfo{
						Properties: garden.Properties{
							"tag:a":      "a-value",
							"tag:b":      "b-value",
							"executor:x": "excluded-value",
							"x":          "another-excluded-value",
						},
					}, nil)
				})

				It("has the tags", func() {
					Ω(executorContainer.Tags).Should(Equal(executor.Tags{
						"a": "a-value",
						"b": "b-value",
					}))
				})
			})

			Context("when the Garden container has mapped ports", func() {
				BeforeEach(func() {
					gardenContainer.InfoReturns(garden.ContainerInfo{
						MappedPorts: []garden.PortMapping{
							{HostPort: 1234, ContainerPort: 5678},
							{HostPort: 4321, ContainerPort: 8765},
						},
					}, nil)
				})

				It("has the ports", func() {
					Ω(executorContainer.Ports).Should(Equal([]executor.PortMapping{
						{HostPort: 1234, ContainerPort: 5678},
						{HostPort: 4321, ContainerPort: 8765},
					}))
				})
			})

			Context("when the Garden container has a log config", func() {
				Context("and the log is valid", func() {
					index := 1
					log := executor.LogConfig{
						Guid:       "my-guid",
						SourceName: "source-name",
						Index:      &index,
					}

					BeforeEach(func() {
						payload, err := json.Marshal(log)
						Ω(err).ShouldNot(HaveOccurred())

						gardenContainer.InfoReturns(garden.ContainerInfo{
							Properties: garden.Properties{
								"executor:log": string(payload),
							},
						}, nil)
					})

					It("has it as its log", func() {
						Ω(executorContainer.Log).Should(Equal(log))
					})
				})

				Context("and the log is invalid", func() {
					BeforeEach(func() {
						gardenContainer.InfoReturns(garden.ContainerInfo{
							Properties: garden.Properties{
								"executor:log": "ß",
							},
						}, nil)
					})

					It("returns an InvalidJSONError", func() {
						Ω(lookupErr).Should(HaveOccurred())
						Ω(lookupErr.Error()).Should(ContainSubstring("executor:log"))
						Ω(lookupErr.Error()).Should(ContainSubstring("ß"))
						Ω(lookupErr.Error()).Should(ContainSubstring("invalid character"))
					})
				})
			})

			Context("when the Garden container has a run result", func() {
				Context("and the run result is valid", func() {
					runResult := executor.ContainerRunResult{
						Guid:          "my-guid",
						Failed:        true,
						FailureReason: "because",
					}

					BeforeEach(func() {
						payload, err := json.Marshal(runResult)
						Ω(err).ShouldNot(HaveOccurred())

						gardenContainer.InfoReturns(garden.ContainerInfo{
							Properties: garden.Properties{
								"executor:result": string(payload),
							},
						}, nil)
					})

					It("has its run result", func() {
						Ω(executorContainer.RunResult).Should(Equal(runResult))
					})
				})

				Context("and the run result is invalid", func() {
					BeforeEach(func() {
						gardenContainer.InfoReturns(garden.ContainerInfo{
							Properties: garden.Properties{
								"executor:result": "ß",
							},
						}, nil)
					})

					It("returns an InvalidJSONError", func() {
						Ω(lookupErr).Should(HaveOccurred())
						Ω(lookupErr.Error()).Should(ContainSubstring("executor:result"))
						Ω(lookupErr.Error()).Should(ContainSubstring("ß"))
						Ω(lookupErr.Error()).Should(ContainSubstring("invalid character"))
					})
				})
			})

			Context("when the container has cpu limits", func() {
				BeforeEach(func() {
					gardenContainer.CurrentCPULimitsReturns(garden.CPULimits{
						LimitInShares: 512,
					}, nil)
				})

				It("returns them", func() {
					Ω(executorContainer.CPUWeight).Should(Equal(uint(50)))
				})
			})

			Context("when getting the info from Garden fails", func() {
				disaster := errors.New("oh no!")

				BeforeEach(func() {
					gardenContainer.InfoReturns(garden.ContainerInfo{}, disaster)
				})

				It("returns the error", func() {
					Ω(lookupErr).Should(Equal(disaster))
				})
			})
		})
	})

	Describe("Create", func() {
		var (
			executorContainer   executor.Container
			fakeGardenContainer *gfakes.FakeContainer

			createdContainer executor.Container
			createErr        error
		)

		BeforeEach(func() {
			executorContainer = executor.Container{
				Guid: "some-guid",
			}

			fakeGardenContainer = new(gfakes.FakeContainer)
		})

		JustBeforeEach(func() {
			createdContainer, createErr = gardenStore.Create(executorContainer)
		})

		Context("when creating the container succeeds", func() {
			BeforeEach(func() {
				fakeGardenClient.CreateReturns(fakeGardenContainer, nil)
			})

			It("does not error", func() {
				Ω(createErr).ShouldNot(HaveOccurred())
			})

			It("returns a created container", func() {
				expectedCreatedContainer := executorContainer
				expectedCreatedContainer.State = executor.StateCreated

				Ω(createdContainer).Should(Equal(expectedCreatedContainer))
			})

			Describe("the exchanged Garden container", func() {
				It("creates it with the state as 'created'", func() {
					containerSpec := fakeGardenClient.CreateArgsForCall(0)
					Ω(containerSpec.Properties["executor:state"]).Should(Equal(string(executor.StateCreated)))
				})

				It("creates it with the owner property", func() {
					containerSpec := fakeGardenClient.CreateArgsForCall(0)
					Ω(containerSpec.Properties["executor:owner"]).Should(Equal(ownerName))
				})

				It("creates it with the guid as the handle", func() {
					containerSpec := fakeGardenClient.CreateArgsForCall(0)
					Ω(containerSpec.Handle).Should(Equal("some-guid"))
				})

				Context("when the Executor container has a rootfs", func() {
					BeforeEach(func() {
						executorContainer.RootFSPath = "focker:///some-rootfs"
					})

					It("creates it with the rootfs", func() {
						containerSpec := fakeGardenClient.CreateArgsForCall(0)
						Ω(containerSpec.RootFSPath).Should(Equal("focker:///some-rootfs"))
					})
				})

				Context("when the Executor container an allocated at time", func() {
					BeforeEach(func() {
						executorContainer.AllocatedAt = 123456789
					})

					It("creates it with the executor:allocated-at property", func() {
						containerSpec := fakeGardenClient.CreateArgsForCall(0)
						Ω(containerSpec.Properties["executor:allocated-at"]).To(Equal("123456789"))
					})
				})

				Context("when the Executor container has a rootfs", func() {
					BeforeEach(func() {
						executorContainer.RootFSPath = "some/root/path"
					})

					It("creates it with the executor:rootfs property", func() {
						containerSpec := fakeGardenClient.CreateArgsForCall(0)
						Ω(containerSpec.Properties["executor:rootfs"]).To(Equal("some/root/path"))
					})
				})

				Context("when the Executor container has a complete-url", func() {
					BeforeEach(func() {
						executorContainer.CompleteURL = "http://callback.example.com/bye"
					})

					It("creates it with the executor:complete-url property", func() {
						containerSpec := fakeGardenClient.CreateArgsForCall(0)
						Ω(containerSpec.Properties["executor:complete-url"]).To(Equal("http://callback.example.com/bye"))
					})
				})

				Context("when the Executor container has Actions", func() {
					actions := []models.ExecutorAction{
						{
							models.RunAction{
								Path: "ls",
							},
						},
					}

					BeforeEach(func() {
						executorContainer.Actions = actions
					})

					It("creates it with the executor:actions property", func() {
						payload, err := json.Marshal(actions)
						Ω(err).ShouldNot(HaveOccurred())

						containerSpec := fakeGardenClient.CreateArgsForCall(0)
						Ω(containerSpec.Properties["executor:actions"]).To(MatchJSON(payload))
					})
				})

				Context("when the Executor container has Env", func() {
					env := []executor.EnvironmentVariable{
						{Name: "FOO", Value: "bar"},
					}

					BeforeEach(func() {
						executorContainer.Env = env
					})

					It("creates it with the executor:env property", func() {
						payload, err := json.Marshal(env)
						Ω(err).ShouldNot(HaveOccurred())

						containerSpec := fakeGardenClient.CreateArgsForCall(0)
						Ω(containerSpec.Properties["executor:env"]).To(MatchJSON(payload))
					})
				})

				Context("when the Executor container has log", func() {
					index := 1
					log := executor.LogConfig{
						Guid:       "my-guid",
						SourceName: "source-name",
						Index:      &index,
					}

					BeforeEach(func() {
						executorContainer.Log = log
					})

					It("creates it with the executor:log property", func() {
						payload, err := json.Marshal(log)
						Ω(err).ShouldNot(HaveOccurred())

						containerSpec := fakeGardenClient.CreateArgsForCall(0)
						Ω(containerSpec.Properties["executor:log"]).To(MatchJSON(payload))
					})
				})

				Context("when the Executor container has a run result", func() {
					runResult := executor.ContainerRunResult{
						Guid:          "my-guid",
						Failed:        true,
						FailureReason: "because",
					}

					BeforeEach(func() {
						executorContainer.RunResult = runResult
					})

					It("creates it with the executor:result property", func() {
						payload, err := json.Marshal(runResult)
						Ω(err).ShouldNot(HaveOccurred())

						containerSpec := fakeGardenClient.CreateArgsForCall(0)
						Ω(containerSpec.Properties["executor:result"]).To(MatchJSON(payload))
					})
				})
			})

			Context("when the Executor container has tags", func() {
				BeforeEach(func() {
					executorContainer.Tags = executor.Tags{
						"tag-one": "one",
						"tag-two": "two",
					}
				})

				It("creates it with the tag properties", func() {
					containerSpec := fakeGardenClient.CreateArgsForCall(0)
					Ω(containerSpec.Properties["tag:tag-one"]).To(Equal("one"))
					Ω(containerSpec.Properties["tag:tag-two"]).To(Equal("two"))
				})
			})

			Context("when the Executor container has mapped ports", func() {
				BeforeEach(func() {
					executorContainer.Ports = []executor.PortMapping{
						{HostPort: 1234, ContainerPort: 5678},
						{HostPort: 4321, ContainerPort: 8765},
					}
				})

				It("creates it with the tag properties", func() {
					Ω(fakeGardenContainer.NetInCallCount()).Should(Equal(2))

					hostPort, containerPort := fakeGardenContainer.NetInArgsForCall(0)
					Ω(hostPort).Should(Equal(uint32(1234)))
					Ω(containerPort).Should(Equal(uint32(5678)))

					hostPort, containerPort = fakeGardenContainer.NetInArgsForCall(1)
					Ω(hostPort).Should(Equal(uint32(4321)))
					Ω(containerPort).Should(Equal(uint32(8765)))
				})

				Context("when mapping ports fails", func() {
					disaster := errors.New("oh no!")

					BeforeEach(func() {
						fakeGardenContainer.NetInReturns(0, 0, disaster)
					})

					It("returns the error", func() {
						Ω(createErr).Should(Equal(disaster))
					})
				})
			})

			Context("when a memory limit is set", func() {
				BeforeEach(func() {
					executorContainer.MemoryMB = 64
				})

				It("sets the memory limit", func() {
					Ω(fakeGardenContainer.LimitMemoryCallCount()).Should(Equal(1))
					Ω(fakeGardenContainer.LimitMemoryArgsForCall(0)).Should(Equal(garden.MemoryLimits{
						LimitInBytes: 64 * 1024 * 1024,
					}))
				})

				It("creates it with the executor:memory-mb property", func() {
					containerSpec := fakeGardenClient.CreateArgsForCall(0)
					Ω(containerSpec.Properties["executor:memory-mb"]).To(Equal("64"))
				})

				It("records its resource consumption", func() {
					Ω(gardenStore.ConsumedResources()).Should(Equal(executor.ExecutorResources{
						MemoryMB:   64,
						Containers: 1,
					}))
				})

				Context("and limiting memory fails", func() {
					disaster := errors.New("oh no!")

					BeforeEach(func() {
						fakeGardenContainer.LimitMemoryReturns(disaster)
					})

					It("returns the error", func() {
						Ω(createErr).Should(Equal(disaster))
					})
				})
			})

			Context("when no memory limit is set", func() {
				BeforeEach(func() {
					executorContainer.MemoryMB = 0
				})

				It("does not apply any", func() {
					Ω(fakeGardenContainer.LimitMemoryCallCount()).Should(BeZero())
				})
			})

			Context("when a disk limit is set", func() {
				BeforeEach(func() {
					executorContainer.DiskMB = 64
				})

				It("sets the disk limit", func() {
					Ω(fakeGardenContainer.LimitDiskCallCount()).Should(Equal(1))
					Ω(fakeGardenContainer.LimitDiskArgsForCall(0)).Should(Equal(garden.DiskLimits{
						ByteHard:  64 * 1024 * 1024,
						InodeHard: inodeLimit,
					}))
				})

				It("records its resource consumption", func() {
					Ω(gardenStore.ConsumedResources()).Should(Equal(executor.ExecutorResources{
						DiskMB:     64,
						Containers: 1,
					}))
				})

				It("creates it with the executor:disk-mb property", func() {
					containerSpec := fakeGardenClient.CreateArgsForCall(0)
					Ω(containerSpec.Properties["executor:disk-mb"]).To(Equal("64"))
				})

				Context("and limiting disk fails", func() {
					disaster := errors.New("oh no!")

					BeforeEach(func() {
						fakeGardenContainer.LimitDiskReturns(disaster)
					})

					It("returns the error", func() {
						Ω(createErr).Should(Equal(disaster))
					})
				})
			})

			Context("when no disk limit is set", func() {
				BeforeEach(func() {
					executorContainer.DiskMB = 0
				})

				It("still sets the inode limit", func() {
					Ω(fakeGardenContainer.LimitDiskCallCount()).Should(Equal(1))
					Ω(fakeGardenContainer.LimitDiskArgsForCall(0)).Should(Equal(garden.DiskLimits{
						InodeHard: inodeLimit,
					}))
				})
			})

			Context("when a cpu limit is set", func() {
				BeforeEach(func() {
					executorContainer.CPUWeight = 50
				})

				It("sets the CPU shares to the ratio of the max shares", func() {
					Ω(fakeGardenContainer.LimitCPUCallCount()).Should(Equal(1))
					Ω(fakeGardenContainer.LimitCPUArgsForCall(0)).Should(Equal(garden.CPULimits{
						LimitInShares: 512,
					}))
				})
			})
		})

		Context("when creating the container fails", func() {
			disaster := errors.New("oh no!")

			BeforeEach(func() {
				fakeGardenClient.CreateReturns(nil, disaster)
			})

			It("returns the error", func() {
				Ω(createErr).Should(Equal(disaster))
			})
		})
	})

	Describe("List", func() {
		It("returns an executor container for each container in garden", func() {
			fakeContainer1 := &gfakes.FakeContainer{
				HandleStub: func() string {
					return "fake-handle-1"
				},

				InfoStub: func() (garden.ContainerInfo, error) {
					return garden.ContainerInfo{
						Properties: garden.Properties{
							store.ContainerStateProperty: string(executor.StateCreated),
						},
					}, nil
				},
			}

			fakeContainer2 := &gfakes.FakeContainer{
				HandleStub: func() string {
					return "fake-handle-2"
				},

				InfoStub: func() (garden.ContainerInfo, error) {
					return garden.ContainerInfo{
						Properties: garden.Properties{
							store.ContainerStateProperty: string(executor.StateCreated),
						},
					}, nil
				},
			}

			fakeGardenClient.ContainersReturns([]garden.Container{
				fakeContainer1,
				fakeContainer2,
			}, nil)

			containers, err := gardenStore.List()
			Ω(err).ShouldNot(HaveOccurred())

			Ω(containers).Should(HaveLen(2))
			Ω(containers[0].Guid).Should(Equal("fake-handle-1"))
			Ω(containers[1].Guid).Should(Equal("fake-handle-2"))

			Ω(containers[0].State).Should(Equal(executor.StateCreated))
			Ω(containers[1].State).Should(Equal(executor.StateCreated))
		})

		It("only queries garden for the containers with the right owner", func() {
			_, err := gardenStore.List()
			Ω(err).ShouldNot(HaveOccurred())

			Ω(fakeGardenClient.ContainersArgsForCall(0)).Should(Equal(garden.Properties{
				store.ContainerOwnerProperty: ownerName,
			}))
		})
	})

	Describe("Destroy", func() {
		Context("when the container exists", func() {
			var fakeContainer *gfakes.FakeContainer

			BeforeEach(func() {
				fakeContainer = new(gfakes.FakeContainer)
				fakeGardenClient.LookupReturns(fakeContainer, nil)
			})

			It("deletes the container", func() {
				gardenStore.Destroy("the-guid")
				Ω(fakeGardenClient.DestroyArgsForCall(0)).Should(Equal("the-guid"))
			})

			It("doesn't return an error", func() {
				err := gardenStore.Destroy("the-guid")
				Ω(err).ShouldNot(HaveOccurred())
			})

			Context("when getting the container info fails", func() {
				BeforeEach(func() {
					fakeContainer.InfoReturns(garden.ContainerInfo{}, errors.New("no-can-do"))
				})

				It("returns an error", func() {
					err := gardenStore.Destroy("the-guid")
					Ω(err).Should(HaveOccurred())
				})
			})
		})

		Context("when the container does not exist", func() {
			BeforeEach(func() {
				fakeGardenClient.LookupReturns(nil, errors.New("aint-no-container"))
			})

			It("returns a container-not-found error", func() {
				err := gardenStore.Destroy("the-guid")
				Ω(err).Should(Equal(store.ErrContainerNotFound))
			})
		})
	})

	Describe("ConsumedResources", func() {
		setupContainerWithLimits := func(memory, disk uint64) {
			fakeGardenClient.LookupReturns(&gfakes.FakeContainer{
				InfoStub: func() (garden.ContainerInfo, error) {
					return garden.ContainerInfo{
						Properties: garden.Properties{
							"executor:memory-mb": strconv.Itoa(int(memory)),
							"executor:disk-mb":   strconv.Itoa(int(disk)),
						},
					}, nil
				},
			}, nil)
		}

		BeforeEach(func() {
			fakeGardenClient.CreateReturns(new(gfakes.FakeContainer), nil)
		})

		Context("when no containers are created", func() {
			It("is zero", func() {
				Ω(gardenStore.ConsumedResources()).Should(BeZero())
			})
		})

		Context("when a container is created", func() {
			BeforeEach(func() {
				_, err := gardenStore.Create(executor.Container{
					Guid:     "first-container",
					MemoryMB: 64,
					DiskMB:   64,
				})
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("reports its memory and disk usage", func() {
				Ω(gardenStore.ConsumedResources()).Should(Equal(executor.ExecutorResources{
					MemoryMB:   64,
					DiskMB:     64,
					Containers: 1,
				}))
			})

			Context("and a second container is created", func() {
				BeforeEach(func() {
					_, err := gardenStore.Create(executor.Container{
						Guid:     "second-container",
						MemoryMB: 32,
						DiskMB:   32,
					})
					Ω(err).ShouldNot(HaveOccurred())
				})

				It("reports the usage of both containers", func() {
					Ω(gardenStore.ConsumedResources()).Should(Equal(executor.ExecutorResources{
						MemoryMB:   96,
						DiskMB:     96,
						Containers: 2,
					}))
				})

				Context("and then destroyed", func() {
					BeforeEach(func() {
						setupContainerWithLimits(32, 32)
						gardenStore.Destroy("second-container")
					})

					It("goes back to the consumed resources of the first container", func() {
						Ω(gardenStore.ConsumedResources()).Should(Equal(executor.ExecutorResources{
							MemoryMB:   64,
							DiskMB:     64,
							Containers: 1,
						}))
					})
				})
			})

			Context("and then destroyed", func() {
				BeforeEach(func() {
					setupContainerWithLimits(64, 64)
					gardenStore.Destroy("first-container")
				})

				It("goes back to zero consumed resources", func() {
					Ω(gardenStore.ConsumedResources()).Should(BeZero())
				})
			})
		})
	})

	Describe("Complete", func() {
		var completeErr error

		JustBeforeEach(func() {
			completeErr = gardenStore.Complete("the-guid", executor.ContainerRunResult{
				Guid:          "the-guid",
				Failed:        true,
				FailureReason: "these things just happen",
			})
		})

		Context("when the container exists", func() {
			var fakeContainer *gfakes.FakeContainer

			BeforeEach(func() {
				fakeContainer = new(gfakes.FakeContainer)
				fakeGardenClient.LookupReturns(fakeContainer, nil)
			})

			It("looks up the container by the guid", func() {
				Ω(fakeGardenClient.LookupArgsForCall(0)).Should(Equal("the-guid"))
			})

			It("stores the run-result on the container before setting the state, so that things polling on the state will see the result", func() {
				key, value := fakeContainer.SetPropertyArgsForCall(0)
				Ω(key).Should(Equal(store.ContainerResultProperty))
				Ω(value).Should(MatchJSON(`{
					"guid": "the-guid",
					"failed":true,
					"failure_reason": "these things just happen"
				}`))
			})

			It("saves the container state as completed", func() {
				key, value := fakeContainer.SetPropertyArgsForCall(1)
				Ω(key).Should(Equal(store.ContainerStateProperty))
				Ω(value).Should(Equal(string(executor.StateCompleted)))
			})

			Context("when setting the property fails", func() {
				BeforeEach(func() {
					fakeContainer.SetPropertyReturns(errors.New("this is bad."))
				})

				It("returns an error", func() {
					Ω(completeErr).Should(HaveOccurred())
				})
			})
		})

		Context("when the container doesn't exist", func() {
			BeforeEach(func() {
				fakeGardenClient.LookupReturns(nil, errors.New("fuuuuuuuu"))
			})

			It("returns a container-not-found error", func() {
				Ω(completeErr).Should(Equal(store.ErrContainerNotFound))
			})
		})
	})

	Describe("GetFiles", func() {
		Context("when the container exists", func() {
			var (
				container  *gfakes.FakeContainer
				fakeStream *gbytes.Buffer
			)

			BeforeEach(func() {
				fakeStream = gbytes.BufferWithBytes([]byte("stuff"))

				container = &gfakes.FakeContainer{}
				container.StreamOutReturns(fakeStream, nil)

				fakeGardenClient.LookupReturns(container, nil)
			})

			It("gets the files", func() {
				stream, err := gardenStore.GetFiles("the-guid", "the-path")
				Ω(err).ShouldNot(HaveOccurred())

				Ω(container.StreamOutArgsForCall(0)).Should(Equal("the-path"))

				bytes, err := ioutil.ReadAll(stream)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(string(bytes)).Should(Equal("stuff"))

				stream.Close()
				Ω(fakeStream.Closed()).Should(BeTrue())
			})
		})

		Context("when the container doesn't exist", func() {
			BeforeEach(func() {
				fakeGardenClient.LookupReturns(nil, errors.New("shiiii"))
			})

			It("returns a container-not-found error", func() {
				_, err := gardenStore.GetFiles("the-guid", "the-path")
				Ω(err).Should(Equal(store.ErrContainerNotFound))
			})
		})
	})

	Describe("Ping", func() {
		Context("when pinging succeeds", func() {
			It("succeeds", func() {
				err := gardenStore.Ping()
				Ω(err).ShouldNot(HaveOccurred())
			})
		})

		Context("when pinging fails", func() {
			disaster := errors.New("welp")

			BeforeEach(func() {
				fakeGardenClient.PingReturns(disaster)
			})

			It("returns a container-not-found error", func() {
				Ω(gardenStore.Ping()).Should(Equal(disaster))
			})
		})
	})

	Describe("TrackContainers", func() {
		It("keeps the consumed resources in sync with garden", func() {
			fakeGardenClient.CreateReturns(&gfakes.FakeContainer{}, nil)

			fakeGardenClient.ContainersReturns([]garden.Container{
				&gfakes.FakeContainer{
					InfoStub: func() (garden.ContainerInfo, error) {
						return garden.ContainerInfo{
							Properties: garden.Properties{
								"executor:memory-mb": "2",
							},
						}, nil
					},
				},
			}, nil)

			runner := gardenStore.TrackContainers(2 * time.Second)

			_, err := gardenStore.Create(executor.Container{
				MemoryMB: 2,
			})
			Ω(err).ShouldNot(HaveOccurred())

			_, err = gardenStore.Create(executor.Container{
				MemoryMB: 5,
			})
			Ω(err).ShouldNot(HaveOccurred())

			process := ifrit.Invoke(runner)

			Ω(gardenStore.ConsumedResources().MemoryMB).Should(Equal(7))

			fakeTimer.Elapse(2 * time.Second)

			By("syncing the memory usage")
			Eventually(func() int { return gardenStore.ConsumedResources().MemoryMB }).Should(Equal(2))

			_, err = gardenStore.Create(executor.Container{
				MemoryMB: 20,
			})
			Ω(err).ShouldNot(HaveOccurred())

			Ω(gardenStore.ConsumedResources().MemoryMB).Should(Equal(22))

			fakeTimer.Elapse(2 * time.Second)

			By("syncing the memory usage. again.")
			Eventually(func() int { return gardenStore.ConsumedResources().MemoryMB }).Should(Equal(2))

			process.Signal(os.Interrupt)
			Eventually(process.Wait()).Should(Receive())
		})
	})
})
