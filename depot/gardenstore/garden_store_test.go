package gardenstore_test

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"net"
	"os"
	"sync"
	"time"

	"github.com/cloudfoundry-incubator/executor"

	"github.com/cloudfoundry-incubator/executor/depot/gardenstore"
	"github.com/cloudfoundry-incubator/executor/depot/gardenstore/fakes"
	"github.com/cloudfoundry-incubator/executor/depot/transformer"
	"github.com/cloudfoundry-incubator/garden"
	gfakes "github.com/cloudfoundry-incubator/garden/fakes"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/dropsonde/log_sender/fake"
	"github.com/cloudfoundry/dropsonde/logs"
	"github.com/pivotal-golang/clock/fakeclock"
	"github.com/pivotal-golang/lager/lagertest"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
)

var _ = Describe("GardenContainerStore", func() {
	var (
		fakeGardenClient *gfakes.FakeClient
		ownerName               = "some-owner-name"
		maxCPUShares     uint64 = 1024
		inodeLimit       uint64 = 2000000
		clock            *fakeclock.FakeClock
		emitter          *fakes.FakeEventEmitter
		fakeLogSender    *fake.FakeLogSender

		logger *lagertest.TestLogger

		gardenStore *gardenstore.GardenStore
	)

	action := &models.RunAction{
		Path: "true",
	}

	BeforeEach(func() {
		fakeGardenClient = new(gfakes.FakeClient)
		clock = fakeclock.NewFakeClock(time.Now())
		emitter = new(fakes.FakeEventEmitter)

		fakeLogSender = fake.NewFakeLogSender()
		logs.Initialize(fakeLogSender)

		logger = lagertest.NewTestLogger("test")

		gardenStore = gardenstore.NewGardenStore(
			fakeGardenClient,
			ownerName,
			maxCPUShares,
			inodeLimit,
			100*time.Millisecond,
			100*time.Millisecond,
			transformer.NewTransformer(nil, nil, nil, nil, nil, nil, os.TempDir(), false, false, clock),
			clock,
			emitter,
		)
	})

	Describe("Lookup", func() {
		var (
			executorContainer executor.Container
			lookupErr         error
		)

		JustBeforeEach(func() {
			executorContainer, lookupErr = gardenStore.Lookup(logger, "some-container-handle")
		})

		Context("when the container doesn't exist", func() {
			BeforeEach(func() {
				fakeGardenClient.LookupReturns(nil, garden.ContainerNotFoundError{})
			})

			It("returns a container-not-found error", func() {
				Ω(lookupErr).Should(Equal(executor.ErrContainerNotFound))
			})
		})

		Context("when lookup fails", func() {
			BeforeEach(func() {
				fakeGardenClient.LookupReturns(nil, errors.New("didn't find it"))
			})

			It("returns the error", func() {
				Ω(lookupErr).Should(MatchError(Equal("didn't find it")))
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

				Context("and it's Running", func() {
					BeforeEach(func() {
						gardenContainer.InfoReturns(garden.ContainerInfo{
							Properties: garden.Properties{
								"executor:state": string(executor.StateRunning),
							},
						}, nil)
					})

					It("has it as its state", func() {
						Ω(executorContainer.State).Should(Equal(executor.StateRunning))
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
						Ω(lookupErr).Should(Equal(gardenstore.InvalidStateError{"bogus-state"}))
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
						Ω(lookupErr).Should(Equal(gardenstore.MalformedPropertyError{
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
						Ω(lookupErr).Should(Equal(gardenstore.MalformedPropertyError{
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
						Ω(lookupErr).Should(Equal(gardenstore.MalformedPropertyError{
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
						Ω(lookupErr).Should(Equal(gardenstore.MalformedPropertyError{
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

			Context("when the container has an executor:egress-rules property", func() {
				Context("and the security group rule is valid", func() {
					var (
						securityGroupRule models.SecurityGroupRule
						egressRules       []models.SecurityGroupRule
					)

					BeforeEach(func() {
						securityGroupRule = models.SecurityGroupRule{
							Protocol:     "tcp",
							Destinations: []string{"0.0.0.0/0"},
							Ports:        []uint16{443},
						}

						egressRules = []models.SecurityGroupRule{securityGroupRule}

						payload, err := json.Marshal(egressRules)
						Ω(err).ShouldNot(HaveOccurred())

						gardenContainer.InfoReturns(garden.ContainerInfo{
							Properties: garden.Properties{
								"executor:egress-rules": string(payload),
							},
						}, nil)
					})

					It("has it as its egress rules", func() {
						Ω(executorContainer.EgressRules).Should(Equal(egressRules))
					})
				})

				Context("and the egress rules are invalid", func() {
					BeforeEach(func() {
						gardenContainer.InfoReturns(garden.ContainerInfo{
							Properties: garden.Properties{
								"executor:egress-rules": "ß",
							},
						}, nil)
					})

					It("returns an InvalidJSONError", func() {
						Ω(lookupErr).Should(HaveOccurred())
						Ω(lookupErr.Error()).Should(ContainSubstring("executor:egress-rules"))
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
						Index:      index,
					}

					BeforeEach(func() {
						payload, err := json.Marshal(log)
						Ω(err).ShouldNot(HaveOccurred())

						gardenContainer.InfoReturns(garden.ContainerInfo{
							Properties: garden.Properties{
								"executor:log-config": string(payload),
							},
						}, nil)
					})

					It("has it as its log", func() {
						Ω(executorContainer.LogConfig).Should(Equal(log))
					})
				})

				Context("and the log is invalid", func() {
					BeforeEach(func() {
						gardenContainer.InfoReturns(garden.ContainerInfo{
							Properties: garden.Properties{
								"executor:log-config": "ß",
							},
						}, nil)
					})

					It("returns an InvalidJSONError", func() {
						Ω(lookupErr).Should(HaveOccurred())
						Ω(lookupErr.Error()).Should(ContainSubstring("executor:log-config"))
						Ω(lookupErr.Error()).Should(ContainSubstring("ß"))
						Ω(lookupErr.Error()).Should(ContainSubstring("invalid character"))
					})
				})
			})

			Context("when the Garden container has a metrics config", func() {
				Context("and the metrics config is valid", func() {
					index := 1
					metricsConfig := executor.MetricsConfig{
						Guid:  "my-guid",
						Index: index,
					}

					BeforeEach(func() {
						payload, err := json.Marshal(metricsConfig)
						Ω(err).ShouldNot(HaveOccurred())

						gardenContainer.InfoReturns(garden.ContainerInfo{
							Properties: garden.Properties{
								"executor:metrics-config": string(payload),
							},
						}, nil)
					})

					It("has it as its metrics config", func() {
						Ω(executorContainer.MetricsConfig).Should(Equal(metricsConfig))
					})
				})

				Context("and the metrics config is invalid", func() {
					BeforeEach(func() {
						gardenContainer.InfoReturns(garden.ContainerInfo{
							Properties: garden.Properties{
								"executor:metrics-config": "ß",
							},
						}, nil)
					})

					It("returns an InvalidJSONError", func() {
						Ω(lookupErr).Should(HaveOccurred())
						Ω(lookupErr.Error()).Should(ContainSubstring("executor:metrics-config"))
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
				Guid:   "some-guid",
				State:  executor.StateInitializing,
				Action: action,

				LogConfig: executor.LogConfig{
					Guid:       "log-guid",
					SourceName: "some-source-name",
					Index:      1,
				},
			}

			fakeGardenContainer = new(gfakes.FakeContainer)
			fakeGardenContainer.HandleReturns("some-guid")
		})

		JustBeforeEach(func() {
			createdContainer, createErr = gardenStore.Create(logger, executorContainer)
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

			It("emits to loggregator", func() {
				logs := fakeLogSender.GetLogs()

				Ω(logs).Should(HaveLen(2))

				emission := logs[0]
				Ω(emission.AppId).Should(Equal("log-guid"))
				Ω(emission.SourceType).Should(Equal("some-source-name"))
				Ω(emission.SourceInstance).Should(Equal("1"))
				Ω(string(emission.Message)).Should(Equal("Creating container"))
				Ω(emission.MessageType).Should(Equal("OUT"))

				emission = logs[1]
				Ω(emission.AppId).Should(Equal("log-guid"))
				Ω(emission.SourceType).Should(Equal("some-source-name"))
				Ω(emission.SourceInstance).Should(Equal("1"))
				Ω(string(emission.Message)).Should(Equal("Successfully created container"))
				Ω(emission.MessageType).Should(Equal("OUT"))
			})

			Describe("the exchanged Garden container", func() {
				It("creates it with the state as 'created'", func() {
					Ω(fakeGardenClient.CreateCallCount()).Should(Equal(1))
					containerSpec := fakeGardenClient.CreateArgsForCall(0)
					Ω(containerSpec.Properties[gardenstore.ContainerStateProperty]).Should(Equal(string(executor.StateCreated)))
				})

				It("creates it with the owner property", func() {
					Ω(fakeGardenClient.CreateCallCount()).Should(Equal(1))
					containerSpec := fakeGardenClient.CreateArgsForCall(0)
					Ω(containerSpec.Properties[gardenstore.ContainerOwnerProperty]).Should(Equal(ownerName))
				})

				It("creates it with the guid as the handle", func() {
					Ω(fakeGardenClient.CreateCallCount()).Should(Equal(1))
					containerSpec := fakeGardenClient.CreateArgsForCall(0)
					Ω(containerSpec.Handle).Should(Equal("some-guid"))
				})

				It("creates it with the executor:action property", func() {
					payload, err := models.MarshalAction(action)
					Ω(err).ShouldNot(HaveOccurred())

					Ω(fakeGardenClient.CreateCallCount()).Should(Equal(1))
					containerSpec := fakeGardenClient.CreateArgsForCall(0)
					Ω(containerSpec.Properties[gardenstore.ContainerActionProperty]).To(MatchJSON(payload))
				})

				Context("when the executorContainer is Privileged", func() {
					BeforeEach(func() {
						executorContainer.Privileged = true
					})

					It("creates a privileged garden container spec", func() {
						Ω(fakeGardenClient.CreateCallCount()).Should(Equal(1))
						containerSpec := fakeGardenClient.CreateArgsForCall(0)
						Ω(containerSpec.Privileged).Should(BeTrue())
					})
				})

				Context("when the executorContainer is not Privileged", func() {
					BeforeEach(func() {
						executorContainer.Privileged = false
					})

					It("creates a privileged garden container spec", func() {
						Ω(fakeGardenClient.CreateCallCount()).Should(Equal(1))
						containerSpec := fakeGardenClient.CreateArgsForCall(0)
						Ω(containerSpec.Privileged).Should(BeFalse())
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
						Ω(fakeGardenClient.CreateCallCount()).Should(Equal(1))
						containerSpec := fakeGardenClient.CreateArgsForCall(0)
						Ω(containerSpec.Env).Should(Equal([]string{"GLOBAL1=VALUE1", "GLOBAL2=VALUE2"}))
					})
				})

				Context("when the Executor container has a rootfs", func() {
					BeforeEach(func() {
						executorContainer.RootFSPath = "focker:///some-rootfs"
					})

					It("creates it with the rootfs", func() {
						Ω(fakeGardenClient.CreateCallCount()).Should(Equal(1))
						containerSpec := fakeGardenClient.CreateArgsForCall(0)
						Ω(containerSpec.RootFSPath).Should(Equal("focker:///some-rootfs"))
					})
				})

				Context("when the Executor container an allocated at time", func() {
					BeforeEach(func() {
						executorContainer.AllocatedAt = 123456789
					})

					It("creates it with the executor:allocated-at property", func() {
						Ω(fakeGardenClient.CreateCallCount()).Should(Equal(1))
						containerSpec := fakeGardenClient.CreateArgsForCall(0)
						Ω(containerSpec.Properties["executor:allocated-at"]).To(Equal("123456789"))
					})
				})

				Context("when the Executor container has a rootfs", func() {
					BeforeEach(func() {
						executorContainer.RootFSPath = "some/root/path"
					})

					It("creates it with the executor:rootfs property", func() {
						Ω(fakeGardenClient.CreateCallCount()).Should(Equal(1))
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

						Ω(fakeGardenClient.CreateCallCount()).Should(Equal(1))
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

						Ω(fakeGardenClient.CreateCallCount()).Should(Equal(1))
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

						Ω(fakeGardenClient.CreateCallCount()).Should(Equal(1))
						containerSpec := fakeGardenClient.CreateArgsForCall(0)
						Ω(containerSpec.Properties["executor:env"]).To(MatchJSON(payload))
					})
				})

				Context("when the Executor container has log", func() {
					index := 1
					log := executor.LogConfig{
						Guid:       "my-guid",
						SourceName: "source-name",
						Index:      index,
					}

					BeforeEach(func() {
						executorContainer.LogConfig = log
					})

					It("creates it with the executor:log-config property", func() {
						payload, err := json.Marshal(log)
						Ω(err).ShouldNot(HaveOccurred())

						Ω(fakeGardenClient.CreateCallCount()).Should(Equal(1))
						containerSpec := fakeGardenClient.CreateArgsForCall(0)
						Ω(containerSpec.Properties["executor:log-config"]).To(MatchJSON(payload))
					})
				})

				Context("when the Executor container has metrics config", func() {
					index := 1
					metricsConfig := executor.MetricsConfig{
						Guid:  "my-guid",
						Index: index,
					}

					BeforeEach(func() {
						executorContainer.MetricsConfig = metricsConfig
					})

					It("creates it with the executor:metrics-config property", func() {
						payload, err := json.Marshal(metricsConfig)
						Ω(err).ShouldNot(HaveOccurred())

						Ω(fakeGardenClient.CreateCallCount()).Should(Equal(1))
						containerSpec := fakeGardenClient.CreateArgsForCall(0)
						Ω(containerSpec.Properties["executor:metrics-config"]).To(MatchJSON(payload))
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

						Ω(fakeGardenClient.CreateCallCount()).Should(Equal(1))
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
					Ω(fakeGardenClient.CreateCallCount()).Should(Equal(1))
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

					It("deletes the container from Garden", func() {
						Ω(fakeGardenClient.DestroyCallCount()).Should(Equal(1))
						Ω(fakeGardenClient.DestroyArgsForCall(0)).Should(Equal("some-guid"))
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

			Context("when the Executor container has egress rules", func() {
				var rules []models.SecurityGroupRule

				BeforeEach(func() {
					rules = []models.SecurityGroupRule{
						{
							Protocol:     "udp",
							Destinations: []string{"0.0.0.0/0"},
							PortRange: &models.PortRange{
								Start: 1,
								End:   1024,
							},
						},
						{
							Protocol:     "tcp",
							Destinations: []string{"1.2.3.4-2.3.4.5"},
							Ports:        []uint16{80, 443},
							Log:          true,
						},
						{
							Protocol:     "icmp",
							Destinations: []string{"1.2.3.4"},
							IcmpInfo:     &models.ICMPInfo{Type: 1, Code: 2},
						},
						{
							Protocol:     "all",
							Destinations: []string{"9.8.7.6", "8.7.6.5"},
							Log:          true,
						},
					}

					executorContainer.EgressRules = rules
				})

				Context("when setting egress rules", func() {
					It("creates it with the egress rules", func() {
						Ω(createErr).ShouldNot(HaveOccurred())
					})

					It("updates egress rules on returned container", func() {
						Ω(fakeGardenContainer.NetOutCallCount()).Should(Equal(4))

						_, expectedNet, err := net.ParseCIDR("0.0.0.0/0")
						Ω(err).ShouldNot(HaveOccurred())

						rule := fakeGardenContainer.NetOutArgsForCall(0)
						Ω(rule.Protocol).Should(Equal(garden.ProtocolUDP))
						Ω(rule.Networks).Should(Equal([]garden.IPRange{garden.IPRangeFromIPNet(expectedNet)}))
						Ω(rule.Ports).Should(Equal([]garden.PortRange{{Start: 1, End: 1024}}))
						Ω(rule.ICMPs).Should(BeNil())
						Ω(rule.Log).Should(BeFalse())

						rule = fakeGardenContainer.NetOutArgsForCall(1)
						Ω(rule.Networks).Should(Equal([]garden.IPRange{{
							Start: net.ParseIP("1.2.3.4"),
							End:   net.ParseIP("2.3.4.5"),
						}}))
						Ω(rule.Ports).Should(Equal([]garden.PortRange{
							garden.PortRangeFromPort(80),
							garden.PortRangeFromPort(443),
						}))
						Ω(rule.ICMPs).Should(BeNil())
						Ω(rule.Log).Should(BeTrue())

						rule = fakeGardenContainer.NetOutArgsForCall(2)
						Ω(rule.Protocol).Should(Equal(garden.ProtocolICMP))
						Ω(rule.Networks).Should(Equal([]garden.IPRange{
							garden.IPRangeFromIP(net.ParseIP("1.2.3.4")),
						}))
						Ω(rule.Ports).Should(BeEmpty())
						Ω(*rule.ICMPs).Should(Equal(garden.ICMPControl{
							Type: garden.ICMPType(1),
							Code: garden.ICMPControlCode(2),
						}))
						Ω(rule.Log).Should(BeFalse())

						rule = fakeGardenContainer.NetOutArgsForCall(3)
						Ω(rule.Protocol).Should(Equal(garden.ProtocolAll))
						Ω(rule.Networks).Should(Equal([]garden.IPRange{
							garden.IPRangeFromIP(net.ParseIP("9.8.7.6")),
							garden.IPRangeFromIP(net.ParseIP("8.7.6.5")),
						}))
						Ω(rule.Ports).Should(BeEmpty())
						Ω(rule.ICMPs).Should(BeNil())
						Ω(rule.Log).Should(BeTrue())
					})
				})

				Context("when security rule is invalid", func() {
					BeforeEach(func() {
						rules = []models.SecurityGroupRule{
							{
								Protocol:     "foo",
								Destinations: []string{"0.0.0.0/0"},
								PortRange: &models.PortRange{
									Start: 1,
									End:   1024,
								},
							},
						}
						executorContainer.EgressRules = rules
					})

					It("returns the error", func() {
						Ω(createErr).Should(HaveOccurred())
						Ω(createErr).Should(Equal(executor.ErrInvalidSecurityGroup))
					})

				})

				Context("when setting egress rules fails", func() {
					disaster := errors.New("NO SECURITY FOR YOU!!!")

					BeforeEach(func() {
						fakeGardenContainer.NetOutReturns(disaster)
					})

					It("returns the error", func() {
						Ω(createErr).Should(HaveOccurred())
					})

					It("deletes the container from Garden", func() {
						Ω(fakeGardenClient.DestroyCallCount()).Should(Equal(1))
						Ω(fakeGardenClient.DestroyArgsForCall(0)).Should(Equal("some-guid"))
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
					Ω(fakeGardenClient.CreateCallCount()).Should(Equal(1))
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

					It("deletes the container from Garden", func() {
						Ω(fakeGardenClient.DestroyCallCount()).Should(Equal(1))
						Ω(fakeGardenClient.DestroyArgsForCall(0)).Should(Equal(executorContainer.Guid))
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
					Ω(fakeGardenClient.CreateCallCount()).Should(Equal(1))
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

					It("deletes the container from Garden", func() {
						Ω(fakeGardenClient.DestroyCallCount()).Should(Equal(1))
						Ω(fakeGardenClient.DestroyArgsForCall(0)).Should(Equal(executorContainer.Guid))
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

				Context("and limiting CPU fails", func() {
					disaster := errors.New("oh no!")

					BeforeEach(func() {
						fakeGardenContainer.LimitCPUReturns(disaster)
					})

					It("returns the error", func() {
						Ω(createErr).Should(Equal(disaster))
					})

					It("deletes the container from Garden", func() {
						Ω(fakeGardenClient.DestroyCallCount()).Should(Equal(1))
						Ω(fakeGardenClient.DestroyArgsForCall(0)).Should(Equal(executorContainer.Guid))
					})
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

				It("deletes the container from Garden", func() {
					Ω(fakeGardenClient.DestroyCallCount()).Should(Equal(1))
					Ω(fakeGardenClient.DestroyArgsForCall(0)).Should(Equal(executorContainer.Guid))
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

			It("emits to loggregator", func() {
				logs := fakeLogSender.GetLogs()

				Ω(logs).Should(HaveLen(2))

				emission := logs[0]
				Ω(emission.AppId).Should(Equal("log-guid"))
				Ω(emission.SourceType).Should(Equal("some-source-name"))
				Ω(emission.SourceInstance).Should(Equal("1"))
				Ω(string(emission.Message)).Should(Equal("Creating container"))
				Ω(emission.MessageType).Should(Equal("OUT"))

				emission = logs[1]
				Ω(emission.AppId).Should(Equal("log-guid"))
				Ω(emission.SourceType).Should(Equal("some-source-name"))
				Ω(emission.SourceInstance).Should(Equal("1"))
				Ω(string(emission.Message)).Should(Equal("Failed to create container"))
				Ω(emission.MessageType).Should(Equal("ERR"))
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
			}

			fakeContainer2 = &gfakes.FakeContainer{
				HandleStub: func() string {
					return "fake-handle-2"
				},
			}

			fakeGardenClient.ContainersReturns([]garden.Container{
				fakeContainer1,
				fakeContainer2,
			}, nil)

			fakeGardenClient.BulkInfoReturns(
				map[string]garden.ContainerInfoEntry{
					"fake-handle-1": garden.ContainerInfoEntry{
						Info: garden.ContainerInfo{
							Properties: garden.Properties{
								gardenstore.ContainerStateProperty: string(executor.StateCreated),
							},
						},
					},
					"fake-handle-2": garden.ContainerInfoEntry{
						Info: garden.ContainerInfo{
							Properties: garden.Properties{
								gardenstore.ContainerStateProperty: string(executor.StateCreated),
							},
						},
					},
				}, nil)

		})

		It("returns an executor container for each container in garden", func() {
			containers, err := gardenStore.List(logger, nil)
			Ω(err).ShouldNot(HaveOccurred())

			Ω(containers).Should(HaveLen(2))
			Ω([]string{containers[0].Guid, containers[1].Guid}).Should(ConsistOf("fake-handle-1", "fake-handle-2"))

			Ω(containers[0].State).Should(Equal(executor.StateCreated))
			Ω(containers[1].State).Should(Equal(executor.StateCreated))
		})

		It("only queries garden for the containers with the right owner", func() {
			_, err := gardenStore.List(logger, nil)
			Ω(err).ShouldNot(HaveOccurred())

			Ω(fakeGardenClient.ContainersArgsForCall(0)).Should(Equal(garden.Properties{
				gardenstore.ContainerOwnerProperty: ownerName,
			}))
		})

		Context("when tags are specified", func() {
			It("filters by the tag properties", func() {
				_, err := gardenStore.List(logger, executor.Tags{"a": "b", "c": "d"})
				Ω(err).ShouldNot(HaveOccurred())

				Ω(fakeGardenClient.ContainersArgsForCall(0)).Should(Equal(garden.Properties{
					gardenstore.ContainerOwnerProperty: ownerName,
					"tag:a": "b",
					"tag:c": "d",
				}))
			})
		})

		Context("when a container's info fails to fetch", func() {
			BeforeEach(func() {
				fakeGardenClient.BulkInfoReturns(
					map[string]garden.ContainerInfoEntry{
						"fake-handle-1": garden.ContainerInfoEntry{
							Err: errors.New("oh no"),
						},
						"fake-handle-2": garden.ContainerInfoEntry{
							Info: garden.ContainerInfo{
								Properties: garden.Properties{
									gardenstore.ContainerStateProperty: string(executor.StateCreated),
								},
							},
						},
					}, nil)

			})

			It("excludes it from the result set", func() {
				containers, err := gardenStore.List(logger, nil)
				Ω(err).ShouldNot(HaveOccurred())

				Ω(containers).Should(HaveLen(1))
				Ω(containers[0].Guid).Should(Equal("fake-handle-2"))

				Ω(containers[0].State).Should(Equal(executor.StateCreated))
			})
		})
	})

	Describe("Destroy", func() {
		const destroySessionPrefix = "test.destroy."
		const freeProcessSessionPrefix = destroySessionPrefix + "freeing-step-process."
		var destroyErr error

		JustBeforeEach(func() {
			destroyErr = gardenStore.Destroy(logger, "the-guid")
		})

		It("doesn't return an error", func() {
			Ω(destroyErr).ShouldNot(HaveOccurred())
		})

		It("destroys the container", func() {
			Ω(fakeGardenClient.DestroyArgsForCall(0)).Should(Equal("the-guid"))
		})

		It("logs its lifecycle", func() {
			Ω(logger).Should(gbytes.Say(destroySessionPrefix + "started"))
			Ω(logger).Should(gbytes.Say(freeProcessSessionPrefix + "started"))
			Ω(logger).Should(gbytes.Say(freeProcessSessionPrefix + "finished"))
			Ω(logger).Should(gbytes.Say(destroySessionPrefix + "succeeded"))
		})

		Context("when the Garden client fails to destroy the given container", func() {
			var gardenDestroyErr = errors.New("destroy-err")

			BeforeEach(func() {
				fakeGardenClient.DestroyReturns(gardenDestroyErr)
			})

			It("returns the Garden error", func() {
				Ω(destroyErr).Should(Equal(gardenDestroyErr))
			})

			It("logs the error", func() {
				Ω(logger).Should(gbytes.Say(destroySessionPrefix + "failed-to-destroy-garden-container"))
			})
		})

		Context("when the Garden client returns ContainerNotFoundError", func() {
			BeforeEach(func() {
				fakeGardenClient.DestroyReturns(garden.ContainerNotFoundError{
					Handle: "some-handle",
				})
			})

			It("doesn't return an error", func() {
				Ω(destroyErr).ShouldNot(HaveOccurred())
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
				stream, err := gardenStore.GetFiles(logger, "the-guid", "the-path")
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
				fakeGardenClient.LookupReturns(nil, garden.ContainerNotFoundError{})
			})

			It("returns a container-not-found error", func() {
				_, err := gardenStore.GetFiles(logger, "the-guid", "the-path")
				Ω(err).Should(Equal(executor.ErrContainerNotFound))
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

	Describe("Run", func() {
		const (
			runSessionPrefix  = "test.run."
			stepSessionPrefix = runSessionPrefix + "run-step-process."
		)

		var (
			processes                    map[string]*gfakes.FakeProcess
			containerProperties          map[string]string
			orderInWhichPropertiesAreSet []string
			gardenContainer              *gfakes.FakeContainer
			executorContainer            executor.Container
			err                          error

			monitorReturns chan int
			runReturns     chan int

			runAction     *models.RunAction
			monitorAction *models.RunAction
			mutex         sync.Mutex
		)

		BeforeEach(func() {
			runAction = &models.RunAction{Path: "run"}
			monitorAction = &models.RunAction{Path: "monitor"}

			mutex.Lock()
			defer mutex.Unlock()

			monitorReturns = make(chan int)
			runReturns = make(chan int)

			executorContainer = executor.Container{
				Action:       runAction,
				Monitor:      monitorAction,
				State:        executor.StateInitializing,
				Guid:         "some-container-handle",
				StartTimeout: 3,
			}

			runSignalled := make(chan struct{})
			monitorSignalled := make(chan struct{})

			processes = make(map[string]*gfakes.FakeProcess)
			processes["run"] = new(gfakes.FakeProcess)
			processes["run"].WaitStub = func() (int, error) {
				select {
				case status := <-runReturns:
					return status, nil
				case <-runSignalled:
					return 143, nil
				}
			}
			processes["run"].SignalStub = func(garden.Signal) error {
				close(runSignalled)
				return nil
			}

			processes["monitor"] = new(gfakes.FakeProcess)
			processes["monitor"].WaitStub = func() (int, error) {
				select {
				case status := <-monitorReturns:
					return status, nil
				case <-monitorSignalled:
					return 143, nil
				}
			}
			processes["monitor"].SignalStub = func(garden.Signal) error {
				close(monitorSignalled)
				return nil
			}

			containerProperties = make(map[string]string)
			containerProperties[gardenstore.ContainerStateProperty] = string(executor.StateCreated)

			orderInWhichPropertiesAreSet = []string{}

			gardenContainer = new(gfakes.FakeContainer)
			gardenContainer.HandleReturns("some-container-handle")
			gardenContainer.SetPropertyStub = func(key, value string) error {
				mutex.Lock()
				containerProperties[key] = value
				orderInWhichPropertiesAreSet = append(orderInWhichPropertiesAreSet, key)
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

		AfterEach(func() {
			close(monitorReturns)
			close(runReturns)
			gardenStore.Stop(logger, "some-container-handle")
			gardenStore.Destroy(logger, "some-container-handle")
		})

		containerStateGetter := func() string {
			mutex.Lock()
			defer mutex.Unlock()
			return containerProperties[gardenstore.ContainerStateProperty]
		}

		containerResult := func() executor.ContainerRunResult {
			mutex.Lock()
			defer mutex.Unlock()
			resultJSON := containerProperties[gardenstore.ContainerResultProperty]
			result := executor.ContainerRunResult{}
			err := json.Unmarshal([]byte(resultJSON), &result)
			Ω(err).ShouldNot(HaveOccurred())
			return result
		}

		Context("when the garden container lookup fails", func() {
			JustBeforeEach(func() {
				executorContainer, err = gardenStore.Create(logger, executorContainer)
				Ω(err).ShouldNot(HaveOccurred())

				gardenStore.Run(logger, executorContainer)
			})

			Context("when the lookup fails because the container is not found", func() {
				BeforeEach(func() {
					fakeGardenClient.LookupReturns(gardenContainer, garden.ContainerNotFoundError{"some-container-handle"})
				})

				It("logs that the container was not found", func() {
					Ω(logger).Should(gbytes.Say(runSessionPrefix + "lookup-failed"))
					Ω(logger).Should(gbytes.Say("some-container-handle"))
				})

				It("does not run the container", func() {
					Consistently(gardenContainer.RunCallCount).Should(Equal(0))
				})
			})

			Context("when the lookup fails for some other reason", func() {
				BeforeEach(func() {
					fakeGardenClient.LookupReturns(gardenContainer, errors.New("whoops"))
				})

				It("logs the error", func() {
					Ω(logger).Should(gbytes.Say(runSessionPrefix + "lookup-failed"))
				})

				It("does not run the container", func() {
					Consistently(gardenContainer.RunCallCount).Should(Equal(0))
				})
			})
		})

		Context("when there is no monitor action", func() {
			BeforeEach(func() {
				executorContainer.Monitor = nil
				executorContainer, err = gardenStore.Create(logger, executorContainer)
				Ω(err).ShouldNot(HaveOccurred())

				err = gardenStore.Run(logger, executorContainer)
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("transitions to running as soon as it starts running", func() {
				Eventually(containerStateGetter).Should(BeEquivalentTo(executor.StateRunning))
				Eventually(emitter.EmitCallCount).Should(Equal(1))
				Ω(emitter.EmitArgsForCall(0).EventType()).Should(Equal(executor.EventTypeContainerRunning))
			})

			Context("when the running action exits succesfully", func() {
				BeforeEach(func() {
					//wait for the run event to have gone through
					Eventually(emitter.EmitCallCount).Should(Equal(1))
					runReturns <- 0
				})

				It("transitions to complete and succeeded", func() {
					Eventually(containerStateGetter).Should(BeEquivalentTo(executor.StateCompleted))
					Eventually(emitter.EmitCallCount).Should(Equal(2))
					Ω(emitter.EmitArgsForCall(1).EventType()).Should(Equal(executor.EventTypeContainerComplete))
					Ω(containerResult().Failed).Should(BeFalse())
				})

				It("logs the successful exit and the transition to complete", func() {
					Eventually(logger).Should(gbytes.Say(stepSessionPrefix + "step-finished-normally"))
					Eventually(logger).Should(gbytes.Say(stepSessionPrefix + "transitioning-to-complete"))
					Eventually(logger).Should(gbytes.Say(stepSessionPrefix + "succeeded-transitioning-to-complete"))
				})
			})

			Context("when the running action exits unsuccesfully", func() {
				BeforeEach(func() {
					//wait for the run event to have gone through
					Eventually(emitter.EmitCallCount).Should(Equal(1))
					runReturns <- 1
				})

				It("transitions to complete and failed", func() {
					Eventually(containerStateGetter).Should(BeEquivalentTo(executor.StateCompleted))
					Eventually(emitter.EmitCallCount).Should(Equal(2))
					Ω(emitter.EmitArgsForCall(1).EventType()).Should(Equal(executor.EventTypeContainerComplete))
					Ω(containerResult().Failed).Should(BeTrue())
					Ω(containerResult().FailureReason).Should(ContainSubstring("Exited with status 1"))
				})

				It("logs the unsuccessful exit", func() {
					Eventually(logger).Should(gbytes.Say(stepSessionPrefix + "step-finished-with-error"))
				})
			})
		})

		Context("when there is a monitor action", func() {
			JustBeforeEach(func() {
				executorContainer, err = gardenStore.Create(logger, executorContainer)
				Ω(err).ShouldNot(HaveOccurred())

				err = gardenStore.Run(logger, executorContainer)
				Ω(err).ShouldNot(HaveOccurred())

				Eventually(clock.WatcherCount).Should(Equal(1))
			})

			Context("when the monitor action succeeds", func() {
				JustBeforeEach(func() {
					clock.Increment(time.Second)
					monitorReturns <- 0
				})

				It("marks the container as running and emits an event", func() {
					Eventually(containerStateGetter).Should(BeEquivalentTo(executor.StateRunning))
					Eventually(emitter.EmitCallCount).Should(Equal(1))
					Ω(emitter.EmitArgsForCall(0).EventType()).Should(Equal(executor.EventTypeContainerRunning))
				})

				It("logs the run session lifecycle", func() {
					Ω(logger).Should(gbytes.Say(runSessionPrefix + "started"))
					Ω(logger).Should(gbytes.Say(runSessionPrefix + "found-garden-container"))
					Ω(logger).Should(gbytes.Say(runSessionPrefix + "stored-step-process"))
					Ω(logger).Should(gbytes.Say(runSessionPrefix + "finished"))
				})

				It("logs that the step process started and transitioned to running", func() {
					Eventually(logger).Should(gbytes.Say(stepSessionPrefix + "started"))
					Eventually(logger).Should(gbytes.Say(stepSessionPrefix + "transitioning-to-running"))
					Eventually(logger).Should(gbytes.Say(stepSessionPrefix + "succeeded-transitioning-to-running"))
				})

				Context("when the monitor action subsequently fails", func() {
					JustBeforeEach(func() {
						Eventually(containerStateGetter).Should(BeEquivalentTo(executor.StateRunning))
						clock.Increment(time.Second)
						monitorReturns <- 1
					})

					It("marks the container completed", func() {
						Eventually(containerStateGetter).Should(BeEquivalentTo(executor.StateCompleted))
						Eventually(emitter.EmitCallCount).Should(Equal(2))
						Ω(emitter.EmitArgsForCall(1).EventType()).Should(Equal(executor.EventTypeContainerComplete))
						Ω(containerResult().Failed).Should(BeTrue())
					})
				})

				Context("when Stop is called", func() {
					const stopSessionPrefix = "test.stop."

					var stopped chan struct{}

					BeforeEach(func() {
						stopped = make(chan struct{})
					})

					JustBeforeEach(func() {
						go func() {
							stopped := stopped
							gardenStore.Stop(logger, executorContainer.Guid)
							close(stopped)
						}()
					})

					It("logs that the step process was signaled and then finished, and was freed", func() {
						Eventually(logger).Should(gbytes.Say(stepSessionPrefix + "signaled"))
						Eventually(logger).Should(gbytes.Say(stepSessionPrefix + "finished"))
					})

					It("logs that the step process was freed", func() {
						freeSessionPrefix := stopSessionPrefix + "freeing-step-process."
						Eventually(logger).Should(gbytes.Say(stopSessionPrefix + "started"))
						Eventually(logger).Should(gbytes.Say(freeSessionPrefix + "started"))
						Eventually(logger).Should(gbytes.Say(freeSessionPrefix + "interrupting-process"))
						Eventually(logger).Should(gbytes.Say(freeSessionPrefix + "finished"))
						Eventually(logger).Should(gbytes.Say(stopSessionPrefix + "finished"))
					})

					It("completes without failure", func() {
						Eventually(containerStateGetter).Should(BeEquivalentTo(executor.StateCompleted))
						Eventually(emitter.EmitCallCount).Should(Equal(2))
						Ω(emitter.EmitArgsForCall(1).EventType()).Should(Equal(executor.EventTypeContainerComplete))
						Ω(containerResult().Failed).Should(BeFalse())
					})

					It("reports in the result that it was stopped", func() {
						Eventually(containerStateGetter).Should(BeEquivalentTo(executor.StateCompleted))
						Eventually(emitter.EmitCallCount).Should(Equal(2))
						Ω(emitter.EmitArgsForCall(1).EventType()).Should(Equal(executor.EventTypeContainerComplete))
						Ω(containerResult().Stopped).Should(BeTrue())
					})

					Context("when the step takes a while to complete", func() {
						var exited chan int

						BeforeEach(func() {
							exited = make(chan int, 1)

							processes["run"].WaitStub = func() (int, error) {
								return <-exited, nil
							}
						})

						It("waits", func() {
							Consistently(stopped).ShouldNot(BeClosed())
							exited <- 1
							Eventually(stopped).ShouldNot(BeClosed())
						})
					})
				})

				Context("when Destroy is called", func() {
					const destroySessionPrefix = "test.destroy."

					var destroyed chan struct{}

					BeforeEach(func() {
						destroyed = make(chan struct{})
					})

					JustBeforeEach(func() {
						go func() {
							destroyed := destroyed
							gardenStore.Destroy(logger, executorContainer.Guid)
							close(destroyed)
						}()
					})

					AfterEach(func() {
						Eventually(destroyed).Should(BeClosed())
					})

					It("logs that the step process was signaled and then finished, and was freed", func() {
						Eventually(logger).Should(gbytes.Say(stepSessionPrefix + "signaled"))
						Eventually(logger).Should(gbytes.Say(stepSessionPrefix + "finished"))
					})

					It("logs that the step process was freed", func() {
						freeSessionPrefix := destroySessionPrefix + "freeing-step-process."
						Eventually(logger).Should(gbytes.Say(destroySessionPrefix + "started"))
						Eventually(logger).Should(gbytes.Say(freeSessionPrefix + "started"))
						Eventually(logger).Should(gbytes.Say(freeSessionPrefix + "interrupting-process"))
						Eventually(logger).Should(gbytes.Say(freeSessionPrefix + "finished"))
						Eventually(logger).Should(gbytes.Say(destroySessionPrefix + "succeeded"))
					})

					It("completes without failure", func() {
						Eventually(containerStateGetter).Should(BeEquivalentTo(executor.StateCompleted))
						Eventually(emitter.EmitCallCount).Should(Equal(2))
						Ω(emitter.EmitArgsForCall(1).EventType()).Should(Equal(executor.EventTypeContainerComplete))
						Ω(containerResult().Failed).Should(BeFalse())
					})

					It("reports in the result that it was stopped", func() {
						Eventually(containerStateGetter).Should(BeEquivalentTo(executor.StateCompleted))
						Eventually(emitter.EmitCallCount).Should(Equal(2))
						Ω(emitter.EmitArgsForCall(1).EventType()).Should(Equal(executor.EventTypeContainerComplete))
						Ω(containerResult().Stopped).Should(BeTrue())
					})
				})
			})

			Context("when monitor persistently fails", func() {
				JustBeforeEach(func() {
					clock.Increment(time.Second)
					monitorReturns <- 1
				})

				It("doesn't transition to running", func() {
					Eventually(containerStateGetter).Should(BeEquivalentTo(executor.StateCreated))
					Eventually(emitter.EmitCallCount).Should(Equal(0))
				})

				Context("when the time to start elapses", func() {
					JustBeforeEach(func() {
						By("ticking out to 3 seconds (note we had just ticked once)")
						for i := 0; i < 3; i++ {
							//ugh, got to wait until the timer is being read from before we increment time
							time.Sleep(10 * time.Millisecond)
							clock.Increment(time.Second)
							monitorReturns <- 1
						}
					})

					It("transitions to completed and failed", func() {
						Eventually(containerStateGetter).Should(BeEquivalentTo(executor.StateCompleted))
						Eventually(emitter.EmitCallCount).Should(Equal(1))
						Ω(emitter.EmitArgsForCall(0).EventType()).Should(Equal(executor.EventTypeContainerComplete))
						Ω(containerResult().Failed).Should(BeTrue())
					})
				})
			})
		})

		Context("when marking the task as complete", func() {
			BeforeEach(func() {
				executorContainer, err = gardenStore.Create(logger, executorContainer)
				Ω(err).ShouldNot(HaveOccurred())

				err = gardenStore.Run(logger, executorContainer)
				Ω(err).ShouldNot(HaveOccurred())
				Eventually(clock.WatcherCount).Should(Equal(1))

				clock.Increment(time.Second)
				monitorReturns <- 0
				Eventually(containerStateGetter).Should(BeEquivalentTo(executor.StateRunning))

				clock.Increment(time.Second)
				monitorReturns <- 1
				Eventually(containerStateGetter).Should(BeEquivalentTo(executor.StateCompleted))
			})

			It("always sets the failure result first, and then the state so that things polling on sate will see the result", func() {
				mutex.Lock()
				defer mutex.Unlock()
				n := len(orderInWhichPropertiesAreSet)
				Ω(n).Should(BeNumerically(">", 2))
				Ω(orderInWhichPropertiesAreSet[n-2]).Should(Equal(gardenstore.ContainerResultProperty))
				Ω(orderInWhichPropertiesAreSet[n-1]).Should(Equal(gardenstore.ContainerStateProperty))
			})
		})
	})

	Describe("Metrics", func() {
		var (
			metrics    map[string]executor.ContainerMetrics
			metricsErr error
		)

		JustBeforeEach(func() {
			metrics, metricsErr = gardenStore.Metrics(logger, []string{"some-container-handle"})
		})

		BeforeEach(func() {
			containerMetrics := garden.Metrics{
				MemoryStat: garden.ContainerMemoryStat{
					TotalRss:          100,
					TotalCache:        12,
					TotalInactiveFile: 1,
				},
				DiskStat: garden.ContainerDiskStat{
					BytesUsed:  222,
					InodesUsed: 333,
				},
				CPUStat: garden.ContainerCPUStat{
					Usage:  123,
					User:   456, // ignored
					System: 789, // ignored
				},
			}

			fakeGardenClient.BulkMetricsReturns(map[string]garden.ContainerMetricsEntry{
				"some-container-handle": garden.ContainerMetricsEntry{
					Metrics: containerMetrics,
					Err:     nil,
				},
			}, nil)
		})

		It("does not error", func() {
			Ω(metricsErr).ShouldNot(HaveOccurred())
		})

		It("gets metrics from garden", func() {
			Ω(fakeGardenClient.BulkMetricsCallCount()).Should(Equal(1))
			Ω(metrics).Should(HaveLen(1))
			Ω(metrics["some-container-handle"]).Should(Equal(executor.ContainerMetrics{
				MemoryUsageInBytes: 111,
				DiskUsageInBytes:   222,
				TimeSpentInCPU:     123,
			}))
		})

		Context("when a container metric entry has an error", func() {
			BeforeEach(func() {
				fakeGardenClient.BulkMetricsReturns(map[string]garden.ContainerMetricsEntry{
					"some-container-handle": garden.ContainerMetricsEntry{
						Err: errors.New("oh no"),
					},
				}, nil)
			})

			It("does not error", func() {
				Ω(metricsErr).ShouldNot(HaveOccurred())
			})

			It("ignores any container with errors", func() {
				Ω(fakeGardenClient.BulkMetricsCallCount()).Should(Equal(1))
				Ω(metrics).Should(HaveLen(0))
			})
		})

		Context("when a bulk metrics returns an error", func() {
			BeforeEach(func() {
				fakeGardenClient.BulkMetricsReturns(nil, errors.New("oh no"))
			})

			It("does not error", func() {
				Ω(metricsErr).Should(HaveOccurred())
			})
		})
	})

	Describe("Transitions", func() {
		var executorContainer executor.Container

		BeforeEach(func() {
			executorContainer = executor.Container{
				Action:  action,
				Monitor: action,
				Guid:    "some-container-handle",
			}

			gardenContainer := new(gfakes.FakeContainer)
			gardenContainer.RunReturns(new(gfakes.FakeProcess), nil)

			fakeGardenClient.LookupReturns(gardenContainer, nil)
			fakeGardenClient.CreateReturns(gardenContainer, nil)
		})

		expectations := []gardenStoreTransitionExpectation{
			{to: "create", from: "non-existent", assertError: "occurs"},
			{to: "create", from: "reserved", assertError: "occurs"},
			{to: "create", from: "initializing", assertError: "does not occur"},
			{to: "create", from: "created", assertError: "occurs"},
			{to: "create", from: "running", assertError: "occurs"},
			{to: "create", from: "completed", assertError: "occurs"},

			{to: "run", from: "non-existent", assertError: "occurs"},
			{to: "run", from: "reserved", assertError: "occurs"},
			{to: "run", from: "initializing", assertError: "occurs"},
			{to: "run", from: "created", assertError: "does not occur"},
			{to: "run", from: "running", assertError: "occurs"},
			{to: "run", from: "completed", assertError: "occurs"},
		}

		for _, expectation := range expectations {
			expectation := expectation
			It("error "+expectation.assertError+" when transitioning from "+expectation.from+" to "+expectation.to, func() {
				expectation.driveFromState(&executorContainer)
				err := expectation.transitionToState(gardenStore, executorContainer)
				expectation.checkErrorResult(err)
			})
		}
	})
})

type gardenStoreTransitionExpectation struct {
	from        string
	to          string
	assertError string
}

func (expectation gardenStoreTransitionExpectation) driveFromState(container *executor.Container) {
	switch expectation.from {
	case "non-existent":

	case "reserved":
		container.State = executor.StateReserved

	case "initializing":
		container.State = executor.StateInitializing

	case "created":
		container.State = executor.StateCreated

	case "running":
		container.State = executor.StateRunning

	case "completed":
		container.State = executor.StateCompleted

	default:
		Fail("unknown 'from' state: " + expectation.from)
	}
}

func (expectation gardenStoreTransitionExpectation) transitionToState(gardenStore *gardenstore.GardenStore, container executor.Container) error {
	switch expectation.to {
	case "create":
		_, err := gardenStore.Create(lagertest.NewTestLogger("test"), container)
		return err

	case "run":
		return gardenStore.Run(lagertest.NewTestLogger("test"), container)

	default:
		Fail("unknown 'to' state: " + expectation.to)
		return nil
	}
}

func (expectation gardenStoreTransitionExpectation) checkErrorResult(err error) {
	switch expectation.assertError {
	case "occurs":
		Ω(err).Should(HaveOccurred())
	case "does not occur":
		Ω(err).ShouldNot(HaveOccurred())
	default:
		Fail("unknown 'assertErr' expectation: " + expectation.assertError)
	}
}
