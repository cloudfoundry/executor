package store_test

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"os"
	"sync"
	"time"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/executor/depot/store"
	"github.com/cloudfoundry-incubator/executor/depot/store/fakes"
	"github.com/cloudfoundry-incubator/executor/depot/transformer"
	garden "github.com/cloudfoundry-incubator/garden/api"
	gfakes "github.com/cloudfoundry-incubator/garden/api/fakes"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/gunk/timeprovider/faketimeprovider"
	"github.com/pivotal-golang/lager"
	"github.com/pivotal-golang/lager/lagertest"
	"github.com/tedsuo/ifrit"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
)

var _ = Describe("GardenContainerStore", func() {
	var (
		gardenStore      *store.GardenStore
		fakeGardenClient *gfakes.FakeClient
		tracker          *fakes.FakeInitializedTracker
		emitter          *fakes.FakeEventEmitter
		logger           lager.Logger

		ownerName           = "some-owner-name"
		inodeLimit   uint64 = 2000000
		maxCPUShares uint64 = 1024

		timeProvider *faketimeprovider.FakeTimeProvider
	)

	action := &models.RunAction{
		Path: "true",
	}

	BeforeEach(func() {
		timeProvider = faketimeprovider.New(time.Now())
		tracker = new(fakes.FakeInitializedTracker)
		emitter = new(fakes.FakeEventEmitter)
		logger = lagertest.NewTestLogger("test")

		fakeGardenClient = new(gfakes.FakeClient)
		gardenStore = store.NewGardenStore(
			fakeGardenClient,
			ownerName,
			maxCPUShares,
			inodeLimit,
			100*time.Millisecond,
			100*time.Millisecond,
			nil,
			transformer.NewTransformer(nil, nil, nil, nil, nil, nil, logger, os.TempDir(), false, false),
			timeProvider,
			tracker,
			emitter,
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

			Context("when the container has an executor:health property", func() {
				Context("and it's Up", func() {
					BeforeEach(func() {
						gardenContainer.InfoReturns(garden.ContainerInfo{
							Properties: garden.Properties{
								"executor:health": string(executor.HealthUp),
							},
						}, nil)
					})

					It("has it as its health", func() {
						Ω(executorContainer.Health).Should(Equal(executor.HealthUp))
					})
				})

				Context("and it's Down", func() {
					BeforeEach(func() {
						gardenContainer.InfoReturns(garden.ContainerInfo{
							Properties: garden.Properties{
								"executor:health": string(executor.HealthDown),
							},
						}, nil)
					})

					It("has it as its health", func() {
						Ω(executorContainer.Health).Should(Equal(executor.HealthDown))
					})
				})

				Context("when it's some other state", func() {
					BeforeEach(func() {
						gardenContainer.InfoReturns(garden.ContainerInfo{
							Properties: garden.Properties{
								"executor:health": "bogus-state",
							},
						}, nil)
					})

					It("returns an InvalidStateError", func() {
						Ω(lookupErr).Should(Equal(store.InvalidHealthError{"bogus-state"}))
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

			Context("when the container has an executor:cpu-weight", func() {
				Context("when it's a valid integer", func() {
					BeforeEach(func() {
						gardenContainer.InfoReturns(garden.ContainerInfo{
							Properties: garden.Properties{
								"executor:cpu-weight": "99",
							},
						}, nil)
					})

					It("has it as its cpu weight", func() {
						Ω(executorContainer.CPUWeight).Should(Equal(uint(99)))
					})
				})

				Context("when it's a bogus value", func() {
					BeforeEach(func() {
						gardenContainer.InfoReturns(garden.ContainerInfo{
							Properties: garden.Properties{
								"executor:cpu-weight": "some-bogus-integer",
							},
						}, nil)
					})

					It("returns a MalformedPropertyError", func() {
						Ω(lookupErr).Should(Equal(store.MalformedPropertyError{
							Property: "executor:cpu-weight",
							Value:    "some-bogus-integer",
						}))
					})
				})
			})

			Context("when the container has an executor:action property", func() {
				Context("and the action is valid", func() {
					BeforeEach(func() {
						payload, err := models.MarshalAction(action)
						Ω(err).ShouldNot(HaveOccurred())

						gardenContainer.InfoReturns(garden.ContainerInfo{
							Properties: garden.Properties{
								"executor:action": string(payload),
							},
						}, nil)
					})

					It("has it as its action", func() {
						Ω(executorContainer.Action).Should(Equal(action))
					})
				})

				Context("and the action is invalid", func() {
					BeforeEach(func() {
						gardenContainer.InfoReturns(garden.ContainerInfo{
							Properties: garden.Properties{
								"executor:action": "ß",
							},
						}, nil)
					})

					It("returns an InvalidJSONError", func() {
						Ω(lookupErr).Should(HaveOccurred())
						Ω(lookupErr.Error()).Should(ContainSubstring("executor:action"))
						Ω(lookupErr.Error()).Should(ContainSubstring("ß"))
						Ω(lookupErr.Error()).Should(ContainSubstring("invalid character"))
					})
				})
			})

			Context("when the container has an executor:setup property", func() {
				Context("and the action is valid", func() {
					action := &models.RunAction{
						Path: "ls",
					}

					BeforeEach(func() {
						payload, err := models.MarshalAction(action)
						Ω(err).ShouldNot(HaveOccurred())

						gardenContainer.InfoReturns(garden.ContainerInfo{
							Properties: garden.Properties{
								"executor:setup": string(payload),
							},
						}, nil)
					})

					It("has it as its setup", func() {
						Ω(executorContainer.Setup).Should(Equal(action))
					})
				})

				Context("and the action is invalid", func() {
					BeforeEach(func() {
						gardenContainer.InfoReturns(garden.ContainerInfo{
							Properties: garden.Properties{
								"executor:setup": "ß",
							},
						}, nil)
					})

					It("returns an InvalidJSONError", func() {
						Ω(lookupErr).Should(HaveOccurred())
						Ω(lookupErr.Error()).Should(ContainSubstring("executor:setup"))
						Ω(lookupErr.Error()).Should(ContainSubstring("ß"))
						Ω(lookupErr.Error()).Should(ContainSubstring("invalid character"))
					})
				})
			})

			Context("when the container has an executor:monitor property", func() {
				Context("and the action is valid", func() {
					action := &models.RunAction{
						Path: "ls",
					}

					BeforeEach(func() {
						payload, err := models.MarshalAction(action)
						Ω(err).ShouldNot(HaveOccurred())

						gardenContainer.InfoReturns(garden.ContainerInfo{
							Properties: garden.Properties{
								"executor:monitor": string(payload),
							},
						}, nil)
					})

					It("has it as its monitor", func() {
						Ω(executorContainer.Monitor).Should(Equal(action))
					})
				})

				Context("and the action is invalid", func() {
					BeforeEach(func() {
						gardenContainer.InfoReturns(garden.ContainerInfo{
							Properties: garden.Properties{
								"executor:monitor": "ß",
							},
						}, nil)
					})

					It("returns an InvalidJSONError", func() {
						Ω(lookupErr).Should(HaveOccurred())
						Ω(lookupErr.Error()).Should(ContainSubstring("executor:monitor"))
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

			Context("when the Garden container has an external IP", func() {
				BeforeEach(func() {
					gardenContainer.InfoReturns(garden.ContainerInfo{
						ExternalIP: "1.2.3.4",
					}, nil)
				})

				It("has the ports", func() {
					Ω(executorContainer.ExternalIP).Should(Equal("1.2.3.4"))
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

		action := &models.RunAction{
			Path: "ls",
		}

		BeforeEach(func() {
			executorContainer = executor.Container{
				Guid: "some-guid",

				Action: action,
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
				expectedCreatedContainer.Health = executor.HealthUnmonitored

				Ω(createdContainer).Should(Equal(expectedCreatedContainer))
			})

			It("records its resource consumption", func() {
				Ω(tracker.InitializeCallCount()).Should(Equal(1))
				Ω(tracker.InitializeArgsForCall(0)).Should(Equal(createdContainer))
			})

			Describe("the exchanged Garden container", func() {
				It("creates it with the state as 'created'", func() {
					containerSpec := fakeGardenClient.CreateArgsForCall(0)
					Ω(containerSpec.Properties[store.ContainerStateProperty]).Should(Equal(string(executor.StateCreated)))
				})

				It("creates it with the owner property", func() {
					containerSpec := fakeGardenClient.CreateArgsForCall(0)
					Ω(containerSpec.Properties[store.ContainerOwnerProperty]).Should(Equal(ownerName))
				})

				It("creates it with the guid as the handle", func() {
					containerSpec := fakeGardenClient.CreateArgsForCall(0)
					Ω(containerSpec.Handle).Should(Equal("some-guid"))
				})

				It("creates it with the executor:action property", func() {
					payload, err := models.MarshalAction(action)
					Ω(err).ShouldNot(HaveOccurred())

					containerSpec := fakeGardenClient.CreateArgsForCall(0)
					Ω(containerSpec.Properties[store.ContainerActionProperty]).To(MatchJSON(payload))
				})

				Context("the Executor container's health", func() {
					Context("with a MonitorAction", func() {
						BeforeEach(func() {
							executorContainer.Monitor = &models.RunAction{Path: "monitor"}
						})

						It("creates it with the health property as down", func() {
							containerSpec := fakeGardenClient.CreateArgsForCall(0)
							Ω(containerSpec.Properties[store.ContainerHealthProperty]).Should(Equal(string(executor.HealthDown)))
						})
					})

					Context("without a MonitorAction", func() {
						BeforeEach(func() {
							executorContainer.Monitor = nil
						})

						It("creates it with the health property as unmonitored", func() {
							containerSpec := fakeGardenClient.CreateArgsForCall(0)
							Ω(containerSpec.Properties[store.ContainerHealthProperty]).Should(Equal(string(executor.HealthUnmonitored)))
						})
					})
				})

				Context("when the Executor container has container-wide env", func() {
					BeforeEach(func() {
						executorContainer.Env = []executor.EnvironmentVariable{
							{Name: "GLOBAL1", Value: "VALUE1"},
							{Name: "GLOBAL2", Value: "VALUE2"},
						}
					})

					It("creates the container with the env", func() {
						containerSpec := fakeGardenClient.CreateArgsForCall(0)
						Ω(containerSpec.Env).Should(Equal([]string{"GLOBAL1=VALUE1", "GLOBAL2=VALUE2"}))
					})
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

				Context("when the Executor container has a Setup", func() {
					action := &models.RunAction{
						Path: "ls",
					}

					BeforeEach(func() {
						executorContainer.Setup = action
					})

					It("creates it with the executor:setup property", func() {
						payload, err := models.MarshalAction(action)
						Ω(err).ShouldNot(HaveOccurred())

						containerSpec := fakeGardenClient.CreateArgsForCall(0)
						Ω(containerSpec.Properties["executor:setup"]).To(MatchJSON(payload))
					})
				})

				Context("when the Executor container has a Monitor", func() {
					action := &models.RunAction{
						Path: "ls",
					}

					BeforeEach(func() {
						executorContainer.Monitor = action
					})

					It("creates it with the executor:monitor property", func() {
						payload, err := models.MarshalAction(action)
						Ω(err).ShouldNot(HaveOccurred())

						containerSpec := fakeGardenClient.CreateArgsForCall(0)
						Ω(containerSpec.Properties["executor:monitor"]).To(MatchJSON(payload))
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

				Context("when mapping ports succeeds", func() {
					BeforeEach(func() {
						fakeGardenContainer.NetInStub = func(hostPort, containerPort uint32) (uint32, uint32, error) {
							return hostPort + 1, containerPort + 1, nil
						}
					})

					It("updates the port mappings on the returned container with what was actually mapped", func() {
						Ω(createdContainer.Ports).Should(Equal([]executor.PortMapping{
							{HostPort: 1235, ContainerPort: 5679},
							{HostPort: 4322, ContainerPort: 8766},
						}))
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

			Context("when gardenContainer.Info succeeds", func() {
				BeforeEach(func() {
					fakeGardenContainer.InfoReturns(garden.ContainerInfo{
						ExternalIP: "fake-ip",
					}, nil)
				})

				It("sets the external IP on the returned container", func() {
					Ω(createdContainer.ExternalIP).Should(Equal("fake-ip"))
				})
			})

			Context("when gardenContainer.Info fails", func() {
				var gardenError = errors.New("garden error")

				BeforeEach(func() {
					fakeGardenContainer.InfoReturns(garden.ContainerInfo{}, gardenError)
				})

				It("propagates the error", func() {
					Ω(createErr).Should(Equal(gardenError))
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
		var (
			fakeContainer1, fakeContainer2 *gfakes.FakeContainer
		)

		BeforeEach(func() {
			fakeContainer1 = &gfakes.FakeContainer{
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

			fakeContainer2 = &gfakes.FakeContainer{
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
		})

		It("returns an executor container for each container in garden", func() {
			containers, err := gardenStore.List(nil)
			Ω(err).ShouldNot(HaveOccurred())

			Ω(containers).Should(HaveLen(2))
			Ω(containers[0].Guid).Should(Equal("fake-handle-1"))
			Ω(containers[1].Guid).Should(Equal("fake-handle-2"))

			Ω(containers[0].State).Should(Equal(executor.StateCreated))
			Ω(containers[1].State).Should(Equal(executor.StateCreated))
		})

		It("only queries garden for the containers with the right owner", func() {
			_, err := gardenStore.List(nil)
			Ω(err).ShouldNot(HaveOccurred())

			Ω(fakeGardenClient.ContainersArgsForCall(0)).Should(Equal(garden.Properties{
				store.ContainerOwnerProperty: ownerName,
			}))
		})

		Context("when tags are specified", func() {
			It("filters by the tag properties", func() {
				_, err := gardenStore.List(executor.Tags{"a": "b", "c": "d"})
				Ω(err).ShouldNot(HaveOccurred())

				Ω(fakeGardenClient.ContainersArgsForCall(0)).Should(Equal(garden.Properties{
					store.ContainerOwnerProperty: ownerName,
					"tag:a": "b",
					"tag:c": "d",
				}))
			})
		})

		Context("when a container's info fails to fetch", func() {
			BeforeEach(func() {
				fakeContainer1.InfoReturns(garden.ContainerInfo{}, errors.New("oh no!"))
			})

			It("excludes it from the result set", func() {
				containers, err := gardenStore.List(nil)
				Ω(err).ShouldNot(HaveOccurred())

				Ω(containers).Should(HaveLen(1))
				Ω(containers[0].Guid).Should(Equal("fake-handle-2"))

				Ω(containers[0].State).Should(Equal(executor.StateCreated))
			})
		})
	})

	Describe("Destroy", func() {
		var destroyErr error

		JustBeforeEach(func() {
			destroyErr = gardenStore.Destroy("the-guid")
		})

		It("doesn't return an error", func() {
			Ω(destroyErr).ShouldNot(HaveOccurred())
		})

		It("releases its resource consumption", func() {
			Ω(tracker.DeinitializeCallCount()).Should(Equal(1))
			Ω(tracker.DeinitializeArgsForCall(0)).Should(Equal("the-guid"))
		})

		It("destroys the container", func() {
			Ω(fakeGardenClient.DestroyArgsForCall(0)).Should(Equal("the-guid"))
		})
	})

	Describe("Complete", func() {
		var completeErr error

		JustBeforeEach(func() {
			completeErr = gardenStore.Complete("the-guid", executor.ContainerRunResult{
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
					"failed":true,
					"failure_reason": "these things just happen"
				}`))
			})

			It("saves the container state as completed", func() {
				key, value := fakeContainer.SetPropertyArgsForCall(1)
				Ω(key).Should(Equal(store.ContainerStateProperty))
				Ω(value).Should(Equal(string(executor.StateCompleted)))
			})

			It("emits a container complete event", func() {
				container, err := gardenStore.Lookup("the-guid")
				Ω(err).ShouldNot(HaveOccurred())

				Ω(emitter.EmitEventCallCount()).Should(Equal(1))
				Ω(emitter.EmitEventArgsForCall(0)).Should(Equal(executor.ContainerCompleteEvent{
					Container: container,
				}))
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
					HandleStub: func() string {
						return "some-handle"
					},

					InfoStub: func() (garden.ContainerInfo, error) {
						return garden.ContainerInfo{
							Properties: garden.Properties{
								"executor:memory-mb": "2",
							},
						}, nil
					},
				},
			}, nil)

			runner := gardenStore.TrackContainers(2*time.Second, logger)

			_, err := gardenStore.Create(executor.Container{
				MemoryMB: 2,
				Action:   action,
			})
			Ω(err).ShouldNot(HaveOccurred())

			_, err = gardenStore.Create(executor.Container{
				MemoryMB: 5,
				Action:   action,
			})
			Ω(err).ShouldNot(HaveOccurred())

			process := ifrit.Invoke(runner)

			Ω(tracker.SyncInitializedCallCount()).Should(Equal(0))

			timeProvider.Increment(2 * time.Second)

			By("syncing the current set of containers")
			Eventually(tracker.SyncInitializedCallCount).Should(Equal(1))
			Ω(tracker.SyncInitializedArgsForCall(0)).Should(Equal([]executor.Container{
				{Guid: "some-handle", MemoryMB: 2, Ports: []executor.PortMapping{}, Tags: executor.Tags{}},
			}))

			timeProvider.Increment(2 * time.Second)

			By("syncing the current set of containers. again.")
			Eventually(tracker.SyncInitializedCallCount).Should(Equal(2))
			Ω(tracker.SyncInitializedArgsForCall(1)).Should(Equal([]executor.Container{
				{Guid: "some-handle", MemoryMB: 2, Ports: []executor.PortMapping{}, Tags: executor.Tags{}},
			}))

			process.Signal(os.Interrupt)
			Eventually(process.Wait()).Should(Receive())
		})
	})

	Describe("Health", func() {
		var (
			processes           map[string]*gfakes.FakeProcess
			containerProperties map[string]string
			gardenContainer     *gfakes.FakeContainer
			waitReturnValue     int
			executorContainer   executor.Container

			runAction     = &models.RunAction{Path: "run"}
			monitorAction = &models.RunAction{Path: "monitor"}
			mutex         = &sync.Mutex{}
		)

		BeforeEach(func() {
			mutex.Lock()
			defer mutex.Unlock()

			waitReturnValue = 0

			executorContainer = executor.Container{
				Action:  runAction,
				Monitor: monitorAction,
				Guid:    "some-container-handle",
			}

			processes = make(map[string]*gfakes.FakeProcess)
			processes["run"] = new(gfakes.FakeProcess)
			processes["monitor"] = new(gfakes.FakeProcess)
			processes["monitor"].WaitStub = func() (int, error) {
				mutex.Lock()
				defer mutex.Unlock()
				return waitReturnValue, nil
			}

			containerProperties = make(map[string]string)

			gardenContainer = new(gfakes.FakeContainer)
			gardenContainer.HandleReturns("some-container-handle")
			gardenContainer.SetPropertyStub = func(key, value string) error {
				mutex.Lock()
				containerProperties[key] = value
				mutex.Unlock()
				return nil
			}
			gardenContainer.InfoStub = func() (garden.ContainerInfo, error) {
				mutex.Lock()
				defer mutex.Unlock()

				props := map[string]string{}
				for k, v := range containerProperties {
					props[k] = v
				}

				return garden.ContainerInfo{
					Properties: props,
				}, nil
			}
			gardenContainer.RunStub = func(processSpec garden.ProcessSpec, _ garden.ProcessIO) (garden.Process, error) {
				mutex.Lock()
				defer mutex.Unlock()
				return processes[processSpec.Path], nil
			}

			fakeGardenClient.LookupReturns(gardenContainer, nil)
			fakeGardenClient.CreateReturns(gardenContainer, nil)
		})

		JustBeforeEach(func() {
			var err error
			executorContainer, err = gardenStore.Create(executorContainer)
			Ω(err).ShouldNot(HaveOccurred())
		})

		Context("when there is no monitor action", func() {
			BeforeEach(func() {
				executorContainer.Monitor = nil
			})

			It("emits the health as unmonitored", func() {
				gardenStore.Run(executorContainer, logger, func(executor.ContainerRunResult) {})

				Eventually(emitter.EmitEventCallCount).Should(Equal(1))

				healthEvent, ok := emitter.EmitEventArgsForCall(0).(executor.ContainerHealthEvent)
				Ω(ok).Should(BeTrue())

				Ω(healthEvent.Container.Health).Should(Equal(executor.HealthUnmonitored))
				Ω(healthEvent.Container.Guid).Should(Equal(executorContainer.Guid))
				Ω(healthEvent.Health).Should(Equal(executor.HealthUnmonitored))
			})
		})

		Context("when the health of the garden container changes", func() {
			It("updates the health of the container", func() {
				gardenStore.Run(executorContainer, logger, func(executor.ContainerRunResult) {})
				Eventually(timeProvider.WatcherCount).Should(Equal(1))

				containerHealth := func() string {
					mutex.Lock()
					defer mutex.Unlock()
					return containerProperties[store.ContainerHealthProperty]
				}

				timeProvider.Increment(time.Second)
				Eventually(containerHealth).Should(Equal(string(executor.HealthUp)))

				mutex.Lock()
				waitReturnValue = 1
				mutex.Unlock()

				timeProvider.Increment(time.Second)
				Eventually(containerHealth).Should(Equal(string(executor.HealthDown)))
			})

			It("emits events to the event hub", func() {
				gardenStore.Run(executorContainer, logger, func(executor.ContainerRunResult) {})
				Eventually(timeProvider.WatcherCount).Should(Equal(1))

				timeProvider.Increment(time.Second)
				Eventually(emitter.EmitEventCallCount).Should(Equal(1))

				healthEvent, ok := emitter.EmitEventArgsForCall(0).(executor.ContainerHealthEvent)
				Ω(ok).Should(BeTrue())

				Ω(healthEvent.Container.Health).Should(Equal(executor.HealthUp))
				Ω(healthEvent.Container.Guid).Should(Equal(executorContainer.Guid))
				Ω(healthEvent.Health).Should(Equal(executor.HealthUp))

				mutex.Lock()
				waitReturnValue = 1
				mutex.Unlock()

				timeProvider.Increment(time.Second)
				Eventually(emitter.EmitEventCallCount).Should(Equal(2))

				healthEvent, ok = emitter.EmitEventArgsForCall(1).(executor.ContainerHealthEvent)
				Ω(ok).Should(BeTrue())

				Ω(healthEvent.Container.Health).Should(Equal(executor.HealthDown))
				Ω(healthEvent.Container.Guid).Should(Equal(executorContainer.Guid))
				Ω(healthEvent.Health).Should(Equal(executor.HealthDown))
			})

			Context("when setting the health fails", func() {
				BeforeEach(func() {
					gardenContainer.SetPropertyStub = nil
					gardenContainer.SetPropertyReturns(errors.New("uh-oh"))
				})

				It("does not emit a health update event", func() {
					gardenStore.Run(executorContainer, logger, func(executor.ContainerRunResult) {})

					Consistently(emitter.EmitEventCallCount).Should(BeZero())
				})
			})
		})
	})
})
