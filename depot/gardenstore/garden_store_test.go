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

		var err error
		gardenStore, err = gardenstore.NewGardenStore(
			fakeGardenClient,
			ownerName,
			maxCPUShares,
			inodeLimit,
			100*time.Millisecond,
			100*time.Millisecond,
			transformer.NewTransformer(nil, nil, nil, nil, nil, nil, os.TempDir(), false, false, clock),
			clock,
			emitter,
			100,
		)
		Expect(err).NotTo(HaveOccurred())
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
				Expect(lookupErr).To(Equal(executor.ErrContainerNotFound))
			})
		})

		Context("when lookup fails", func() {
			BeforeEach(func() {
				fakeGardenClient.LookupReturns(nil, errors.New("didn't find it"))
			})

			It("returns the error", func() {
				Expect(lookupErr).To(MatchError(Equal("didn't find it")))
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
				Expect(lookupErr).NotTo(HaveOccurred())
			})

			It("has the Garden container handle as its container guid", func() {
				Expect(executorContainer.Guid).To(Equal("some-container-handle"))
			})

			It("looked up by the given guid", func() {
				Expect(fakeGardenClient.LookupArgsForCall(0)).To(Equal("some-container-handle"))
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
						Expect(executorContainer.State).To(Equal(executor.StateReserved))
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
						Expect(executorContainer.State).To(Equal(executor.StateInitializing))
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
						Expect(executorContainer.State).To(Equal(executor.StateCreated))
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
						Expect(executorContainer.State).To(Equal(executor.StateRunning))
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
						Expect(executorContainer.State).To(Equal(executor.StateCompleted))
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
						Expect(lookupErr).To(Equal(gardenstore.InvalidStateError{"bogus-state"}))
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
						Expect(executorContainer.AllocatedAt).To(Equal(int64(123)))
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
						Expect(lookupErr).To(Equal(gardenstore.MalformedPropertyError{
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
						Expect(executorContainer.MemoryMB).To(Equal(1024))
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
						Expect(lookupErr).To(Equal(gardenstore.MalformedPropertyError{
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
						Expect(executorContainer.DiskMB).To(Equal(2048))
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
						Expect(lookupErr).To(Equal(gardenstore.MalformedPropertyError{
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
						Expect(executorContainer.CPUWeight).To(Equal(uint(99)))
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
						Expect(lookupErr).To(Equal(gardenstore.MalformedPropertyError{
							Property: "executor:cpu-weight",
							Value:    "some-bogus-integer",
						}))

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
					Expect(executorContainer.Tags).To(Equal(executor.Tags{
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
					Expect(executorContainer.Ports).To(Equal([]executor.PortMapping{
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
					Expect(executorContainer.ExternalIP).To(Equal("1.2.3.4"))
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
						Expect(err).NotTo(HaveOccurred())

						gardenContainer.InfoReturns(garden.ContainerInfo{
							Properties: garden.Properties{
								"executor:log-config": string(payload),
							},
						}, nil)
					})

					It("has it as its log", func() {
						Expect(executorContainer.LogConfig).To(Equal(log))
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
						Expect(lookupErr).To(HaveOccurred())
						Expect(lookupErr.Error()).To(ContainSubstring("executor:log-config"))
						Expect(lookupErr.Error()).To(ContainSubstring("ß"))
						Expect(lookupErr.Error()).To(ContainSubstring("invalid character"))
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
						Expect(err).NotTo(HaveOccurred())

						gardenContainer.InfoReturns(garden.ContainerInfo{
							Properties: garden.Properties{
								"executor:metrics-config": string(payload),
							},
						}, nil)
					})

					It("has it as its metrics config", func() {
						Expect(executorContainer.MetricsConfig).To(Equal(metricsConfig))
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
						Expect(lookupErr).To(HaveOccurred())
						Expect(lookupErr.Error()).To(ContainSubstring("executor:metrics-config"))
						Expect(lookupErr.Error()).To(ContainSubstring("ß"))
						Expect(lookupErr.Error()).To(ContainSubstring("invalid character"))
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
						Expect(err).NotTo(HaveOccurred())

						gardenContainer.InfoReturns(garden.ContainerInfo{
							Properties: garden.Properties{
								"executor:result": string(payload),
							},
						}, nil)
					})

					It("has its run result", func() {
						Expect(executorContainer.RunResult).To(Equal(runResult))
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
						Expect(lookupErr).To(HaveOccurred())
						Expect(lookupErr.Error()).To(ContainSubstring("executor:result"))
						Expect(lookupErr.Error()).To(ContainSubstring("ß"))
						Expect(lookupErr.Error()).To(ContainSubstring("invalid character"))
					})
				})
			})

			Context("when getting the info from Garden fails", func() {
				disaster := errors.New("oh no!")

				BeforeEach(func() {
					gardenContainer.InfoReturns(garden.ContainerInfo{}, disaster)
				})

				It("returns the error", func() {
					Expect(lookupErr).To(Equal(disaster))
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
			User: "me",
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
				Expect(createErr).NotTo(HaveOccurred())
			})

			It("returns a created container", func() {
				expectedCreatedContainer := executorContainer
				expectedCreatedContainer.State = executor.StateCreated

				Expect(createdContainer).To(Equal(expectedCreatedContainer))
			})

			It("emits to loggregator", func() {
				logs := fakeLogSender.GetLogs()

				Expect(logs).To(HaveLen(2))

				emission := logs[0]
				Expect(emission.AppId).To(Equal("log-guid"))
				Expect(emission.SourceType).To(Equal("some-source-name"))
				Expect(emission.SourceInstance).To(Equal("1"))
				Expect(string(emission.Message)).To(Equal("Creating container"))
				Expect(emission.MessageType).To(Equal("OUT"))

				emission = logs[1]
				Expect(emission.AppId).To(Equal("log-guid"))
				Expect(emission.SourceType).To(Equal("some-source-name"))
				Expect(emission.SourceInstance).To(Equal("1"))
				Expect(string(emission.Message)).To(Equal("Successfully created container"))
				Expect(emission.MessageType).To(Equal("OUT"))
			})

			Describe("the exchanged Garden container", func() {
				It("creates it with the state as 'created'", func() {
					Expect(fakeGardenClient.CreateCallCount()).To(Equal(1))
					containerSpec := fakeGardenClient.CreateArgsForCall(0)
					Expect(containerSpec.Properties[gardenstore.ContainerStateProperty]).To(Equal(string(executor.StateCreated)))
				})

				It("creates it with the owner property", func() {
					Expect(fakeGardenClient.CreateCallCount()).To(Equal(1))
					containerSpec := fakeGardenClient.CreateArgsForCall(0)
					Expect(containerSpec.Properties[gardenstore.ContainerOwnerProperty]).To(Equal(ownerName))
				})

				It("creates it with the guid as the handle", func() {
					Expect(fakeGardenClient.CreateCallCount()).To(Equal(1))
					containerSpec := fakeGardenClient.CreateArgsForCall(0)
					Expect(containerSpec.Handle).To(Equal("some-guid"))
				})

				Context("when the executorContainer is Privileged", func() {
					BeforeEach(func() {
						executorContainer.Privileged = true
					})

					It("creates a privileged garden container spec", func() {
						Expect(fakeGardenClient.CreateCallCount()).To(Equal(1))
						containerSpec := fakeGardenClient.CreateArgsForCall(0)
						Expect(containerSpec.Privileged).To(BeTrue())
					})
				})

				Context("when the executorContainer is not Privileged", func() {
					BeforeEach(func() {
						executorContainer.Privileged = false
					})

					It("creates a privileged garden container spec", func() {
						Expect(fakeGardenClient.CreateCallCount()).To(Equal(1))
						containerSpec := fakeGardenClient.CreateArgsForCall(0)
						Expect(containerSpec.Privileged).To(BeFalse())
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
						Expect(fakeGardenClient.CreateCallCount()).To(Equal(1))
						containerSpec := fakeGardenClient.CreateArgsForCall(0)
						Expect(containerSpec.Env).To(Equal([]string{"GLOBAL1=VALUE1", "GLOBAL2=VALUE2"}))
					})
				})

				Context("when the Executor container has a rootfs", func() {
					BeforeEach(func() {
						executorContainer.RootFSPath = "focker:///some-rootfs"
					})

					It("creates it with the rootfs", func() {
						Expect(fakeGardenClient.CreateCallCount()).To(Equal(1))
						containerSpec := fakeGardenClient.CreateArgsForCall(0)
						Expect(containerSpec.RootFSPath).To(Equal("focker:///some-rootfs"))
					})
				})

				Context("when the Executor container an allocated at time", func() {
					BeforeEach(func() {
						executorContainer.AllocatedAt = 123456789
					})

					It("creates it with the executor:allocated-at property", func() {
						Expect(fakeGardenClient.CreateCallCount()).To(Equal(1))
						containerSpec := fakeGardenClient.CreateArgsForCall(0)
						Expect(containerSpec.Properties["executor:allocated-at"]).To(Equal("123456789"))
					})
				})

				Context("when the Executor container has a rootfs", func() {
					BeforeEach(func() {
						executorContainer.RootFSPath = "some/root/path"
					})

					It("creates it with the executor:rootfs property", func() {
						Expect(fakeGardenClient.CreateCallCount()).To(Equal(1))
						containerSpec := fakeGardenClient.CreateArgsForCall(0)
						Expect(containerSpec.Properties["executor:rootfs"]).To(Equal("some/root/path"))
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
						Expect(err).NotTo(HaveOccurred())

						Expect(fakeGardenClient.CreateCallCount()).To(Equal(1))
						containerSpec := fakeGardenClient.CreateArgsForCall(0)
						Expect(containerSpec.Properties["executor:log-config"]).To(MatchJSON(payload))
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
						Expect(err).NotTo(HaveOccurred())

						Expect(fakeGardenClient.CreateCallCount()).To(Equal(1))
						containerSpec := fakeGardenClient.CreateArgsForCall(0)
						Expect(containerSpec.Properties["executor:metrics-config"]).To(MatchJSON(payload))
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
						Expect(err).NotTo(HaveOccurred())

						Expect(fakeGardenClient.CreateCallCount()).To(Equal(1))
						containerSpec := fakeGardenClient.CreateArgsForCall(0)
						Expect(containerSpec.Properties["executor:result"]).To(MatchJSON(payload))
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
					Expect(fakeGardenClient.CreateCallCount()).To(Equal(1))
					containerSpec := fakeGardenClient.CreateArgsForCall(0)
					Expect(containerSpec.Properties["tag:tag-one"]).To(Equal("one"))
					Expect(containerSpec.Properties["tag:tag-two"]).To(Equal("two"))
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
					Expect(fakeGardenContainer.NetInCallCount()).To(Equal(2))

					hostPort, containerPort := fakeGardenContainer.NetInArgsForCall(0)
					Expect(hostPort).To(Equal(uint32(1234)))
					Expect(containerPort).To(Equal(uint32(5678)))

					hostPort, containerPort = fakeGardenContainer.NetInArgsForCall(1)
					Expect(hostPort).To(Equal(uint32(4321)))
					Expect(containerPort).To(Equal(uint32(8765)))
				})

				Context("when mapping ports fails", func() {
					disaster := errors.New("oh no!")

					BeforeEach(func() {
						fakeGardenContainer.NetInReturns(0, 0, disaster)
					})

					It("returns the error", func() {
						Expect(createErr).To(Equal(disaster))
					})

					It("deletes the container from Garden", func() {
						Expect(fakeGardenClient.DestroyCallCount()).To(Equal(1))
						Expect(fakeGardenClient.DestroyArgsForCall(0)).To(Equal("some-guid"))
					})
				})

				Context("when mapping ports succeeds", func() {
					BeforeEach(func() {
						fakeGardenContainer.NetInStub = func(hostPort, containerPort uint32) (uint32, uint32, error) {
							return hostPort + 1, containerPort + 1, nil
						}
					})

					It("updates the port mappings on the returned container with what was actually mapped", func() {
						Expect(createdContainer.Ports).To(Equal([]executor.PortMapping{
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
						Expect(createErr).NotTo(HaveOccurred())
					})

					It("updates egress rules on returned container", func() {
						Expect(fakeGardenContainer.NetOutCallCount()).To(Equal(4))

						_, expectedNet, err := net.ParseCIDR("0.0.0.0/0")
						Expect(err).NotTo(HaveOccurred())

						rule := fakeGardenContainer.NetOutArgsForCall(0)
						Expect(rule.Protocol).To(Equal(garden.ProtocolUDP))
						Expect(rule.Networks).To(Equal([]garden.IPRange{garden.IPRangeFromIPNet(expectedNet)}))
						Expect(rule.Ports).To(Equal([]garden.PortRange{{Start: 1, End: 1024}}))
						Expect(rule.ICMPs).To(BeNil())
						Expect(rule.Log).To(BeFalse())

						rule = fakeGardenContainer.NetOutArgsForCall(1)
						Expect(rule.Networks).To(Equal([]garden.IPRange{{
							Start: net.ParseIP("1.2.3.4"),
							End:   net.ParseIP("2.3.4.5"),
						}}))

						Expect(rule.Ports).To(Equal([]garden.PortRange{
							garden.PortRangeFromPort(80),
							garden.PortRangeFromPort(443),
						}))

						Expect(rule.ICMPs).To(BeNil())
						Expect(rule.Log).To(BeTrue())

						rule = fakeGardenContainer.NetOutArgsForCall(2)
						Expect(rule.Protocol).To(Equal(garden.ProtocolICMP))
						Expect(rule.Networks).To(Equal([]garden.IPRange{
							garden.IPRangeFromIP(net.ParseIP("1.2.3.4")),
						}))

						Expect(rule.Ports).To(BeEmpty())
						Expect(*rule.ICMPs).To(Equal(garden.ICMPControl{
							Type: garden.ICMPType(1),
							Code: garden.ICMPControlCode(2),
						}))

						Expect(rule.Log).To(BeFalse())

						rule = fakeGardenContainer.NetOutArgsForCall(3)
						Expect(rule.Protocol).To(Equal(garden.ProtocolAll))
						Expect(rule.Networks).To(Equal([]garden.IPRange{
							garden.IPRangeFromIP(net.ParseIP("9.8.7.6")),
							garden.IPRangeFromIP(net.ParseIP("8.7.6.5")),
						}))

						Expect(rule.Ports).To(BeEmpty())
						Expect(rule.ICMPs).To(BeNil())
						Expect(rule.Log).To(BeTrue())
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
						Expect(createErr).To(HaveOccurred())
						Expect(createErr).To(Equal(executor.ErrInvalidSecurityGroup))
					})

				})

				Context("when setting egress rules fails", func() {
					disaster := errors.New("NO SECURITY FOR YOU!!!")

					BeforeEach(func() {
						fakeGardenContainer.NetOutReturns(disaster)
					})

					It("returns the error", func() {
						Expect(createErr).To(HaveOccurred())
					})

					It("deletes the container from Garden", func() {
						Expect(fakeGardenClient.DestroyCallCount()).To(Equal(1))
						Expect(fakeGardenClient.DestroyArgsForCall(0)).To(Equal("some-guid"))
					})
				})
			})

			Context("when a memory limit is set", func() {
				BeforeEach(func() {
					executorContainer.MemoryMB = 64
				})

				It("sets the memory limit", func() {
					Expect(fakeGardenContainer.LimitMemoryCallCount()).To(Equal(1))
					Expect(fakeGardenContainer.LimitMemoryArgsForCall(0)).To(Equal(garden.MemoryLimits{
						LimitInBytes: 64 * 1024 * 1024,
					}))

				})

				It("creates it with the executor:memory-mb property", func() {
					Expect(fakeGardenClient.CreateCallCount()).To(Equal(1))
					containerSpec := fakeGardenClient.CreateArgsForCall(0)
					Expect(containerSpec.Properties["executor:memory-mb"]).To(Equal("64"))
				})

				Context("and limiting memory fails", func() {
					disaster := errors.New("oh no!")

					BeforeEach(func() {
						fakeGardenContainer.LimitMemoryReturns(disaster)
					})

					It("returns the error", func() {
						Expect(createErr).To(Equal(disaster))
					})

					It("deletes the container from Garden", func() {
						Expect(fakeGardenClient.DestroyCallCount()).To(Equal(1))
						Expect(fakeGardenClient.DestroyArgsForCall(0)).To(Equal(executorContainer.Guid))
					})
				})
			})

			Context("when no memory limit is set", func() {
				BeforeEach(func() {
					executorContainer.MemoryMB = 0
				})

				It("does not apply any", func() {
					Expect(fakeGardenContainer.LimitMemoryCallCount()).To(BeZero())
				})
			})

			Context("when a disk limit is set", func() {
				BeforeEach(func() {
					executorContainer.DiskMB = 64
				})

				It("sets the disk limit", func() {
					Expect(fakeGardenContainer.LimitDiskCallCount()).To(Equal(1))
					Expect(fakeGardenContainer.LimitDiskArgsForCall(0)).To(Equal(garden.DiskLimits{
						ByteHard:  64 * 1024 * 1024,
						InodeHard: inodeLimit,
					}))

				})

				It("creates it with the executor:disk-mb property", func() {
					Expect(fakeGardenClient.CreateCallCount()).To(Equal(1))
					containerSpec := fakeGardenClient.CreateArgsForCall(0)
					Expect(containerSpec.Properties["executor:disk-mb"]).To(Equal("64"))
				})

				Context("and limiting disk fails", func() {
					disaster := errors.New("oh no!")

					BeforeEach(func() {
						fakeGardenContainer.LimitDiskReturns(disaster)
					})

					It("returns the error", func() {
						Expect(createErr).To(Equal(disaster))
					})

					It("deletes the container from Garden", func() {
						Expect(fakeGardenClient.DestroyCallCount()).To(Equal(1))
						Expect(fakeGardenClient.DestroyArgsForCall(0)).To(Equal(executorContainer.Guid))
					})
				})
			})

			Context("when no disk limit is set", func() {
				BeforeEach(func() {
					executorContainer.DiskMB = 0
				})

				It("still sets the inode limit", func() {
					Expect(fakeGardenContainer.LimitDiskCallCount()).To(Equal(1))
					Expect(fakeGardenContainer.LimitDiskArgsForCall(0)).To(Equal(garden.DiskLimits{
						InodeHard: inodeLimit,
					}))

				})
			})

			Context("when a cpu limit is set", func() {
				BeforeEach(func() {
					executorContainer.CPUWeight = 50
				})

				It("sets the CPU shares to the ratio of the max shares", func() {
					Expect(fakeGardenContainer.LimitCPUCallCount()).To(Equal(1))
					Expect(fakeGardenContainer.LimitCPUArgsForCall(0)).To(Equal(garden.CPULimits{
						LimitInShares: 512,
					}))

				})

				Context("and limiting CPU fails", func() {
					disaster := errors.New("oh no!")

					BeforeEach(func() {
						fakeGardenContainer.LimitCPUReturns(disaster)
					})

					It("returns the error", func() {
						Expect(createErr).To(Equal(disaster))
					})

					It("deletes the container from Garden", func() {
						Expect(fakeGardenClient.DestroyCallCount()).To(Equal(1))
						Expect(fakeGardenClient.DestroyArgsForCall(0)).To(Equal(executorContainer.Guid))
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
					Expect(createdContainer.ExternalIP).To(Equal("fake-ip"))
				})
			})

			Context("when gardenContainer.Info fails", func() {
				var gardenError = errors.New("garden error")

				BeforeEach(func() {
					fakeGardenContainer.InfoReturns(garden.ContainerInfo{}, gardenError)
				})

				It("propagates the error", func() {
					Expect(createErr).To(Equal(gardenError))
				})

				It("deletes the container from Garden", func() {
					Expect(fakeGardenClient.DestroyCallCount()).To(Equal(1))
					Expect(fakeGardenClient.DestroyArgsForCall(0)).To(Equal(executorContainer.Guid))
				})
			})
		})

		Context("when creating the container fails", func() {
			disaster := errors.New("oh no!")

			BeforeEach(func() {
				fakeGardenClient.CreateReturns(nil, disaster)
			})

			It("returns the error", func() {
				Expect(createErr).To(Equal(disaster))
			})

			It("emits to loggregator", func() {
				logs := fakeLogSender.GetLogs()

				Expect(logs).To(HaveLen(2))

				emission := logs[0]
				Expect(emission.AppId).To(Equal("log-guid"))
				Expect(emission.SourceType).To(Equal("some-source-name"))
				Expect(emission.SourceInstance).To(Equal("1"))
				Expect(string(emission.Message)).To(Equal("Creating container"))
				Expect(emission.MessageType).To(Equal("OUT"))

				emission = logs[1]
				Expect(emission.AppId).To(Equal("log-guid"))
				Expect(emission.SourceType).To(Equal("some-source-name"))
				Expect(emission.SourceInstance).To(Equal("1"))
				Expect(string(emission.Message)).To(Equal("Failed to create container"))
				Expect(emission.MessageType).To(Equal("ERR"))
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
			Expect(err).NotTo(HaveOccurred())

			Expect(containers).To(HaveLen(2))
			Expect([]string{containers[0].Guid, containers[1].Guid}).To(ConsistOf("fake-handle-1", "fake-handle-2"))

			Expect(containers[0].State).To(Equal(executor.StateCreated))
			Expect(containers[1].State).To(Equal(executor.StateCreated))

			Expect(fakeGardenClient.BulkInfoCallCount()).To(Equal(1))
			Expect(fakeGardenClient.BulkInfoArgsForCall(0)).To(ConsistOf("fake-handle-1", "fake-handle-2"))
		})

		It("only queries garden for the containers with the right owner", func() {
			_, err := gardenStore.List(logger, nil)
			Expect(err).NotTo(HaveOccurred())

			Expect(fakeGardenClient.ContainersArgsForCall(0)).To(Equal(garden.Properties{
				gardenstore.ContainerOwnerProperty: ownerName,
			}))
		})

		Context("when tags are specified", func() {
			It("filters by the tag properties", func() {
				_, err := gardenStore.List(logger, executor.Tags{"a": "b", "c": "d"})
				Expect(err).NotTo(HaveOccurred())

				Expect(fakeGardenClient.ContainersArgsForCall(0)).To(Equal(garden.Properties{
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
							Err: garden.NewError("oh no"),
						},
						"fake-handle-2": garden.ContainerInfoEntry{
							Info: garden.ContainerInfo{
								Properties: garden.Properties{
									gardenstore.ContainerStateProperty: string(executor.StateCreated),
								},
							},
						},
					},
					nil,
				)
			})

			It("excludes it from the result set", func() {
				containers, err := gardenStore.List(logger, nil)
				Expect(err).NotTo(HaveOccurred())

				Expect(containers).To(HaveLen(1))
				Expect(containers[0].Guid).To(Equal("fake-handle-2"))

				Expect(containers[0].State).To(Equal(executor.StateCreated))
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
			Expect(destroyErr).NotTo(HaveOccurred())
		})

		It("destroys the container", func() {
			Expect(fakeGardenClient.DestroyArgsForCall(0)).To(Equal("the-guid"))
		})

		It("logs its lifecycle", func() {
			Expect(logger).To(gbytes.Say(destroySessionPrefix + "started"))
			Expect(logger).To(gbytes.Say(freeProcessSessionPrefix + "started"))
			Expect(logger).To(gbytes.Say(freeProcessSessionPrefix + "finished"))
			Expect(logger).To(gbytes.Say(destroySessionPrefix + "succeeded"))
		})

		Context("when the Garden client fails to destroy the given container", func() {
			var gardenDestroyErr = errors.New("destroy-err")

			BeforeEach(func() {
				fakeGardenClient.DestroyReturns(gardenDestroyErr)
			})

			It("returns the Garden error", func() {
				Expect(destroyErr).To(Equal(gardenDestroyErr))
			})

			It("logs the error", func() {
				Expect(logger).To(gbytes.Say(destroySessionPrefix + "failed-to-destroy-garden-container"))
			})
		})

		Context("when the Garden client returns ContainerNotFoundError", func() {
			BeforeEach(func() {
				fakeGardenClient.DestroyReturns(garden.ContainerNotFoundError{
					Handle: "some-handle",
				})
			})

			It("doesn't return an error", func() {
				Expect(destroyErr).NotTo(HaveOccurred())
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
				Expect(err).NotTo(HaveOccurred())

				Expect(container.StreamOutArgsForCall(0)).To(Equal(garden.StreamOutSpec{Path: "the-path", User: "vcap"}))

				bytes, err := ioutil.ReadAll(stream)
				Expect(err).NotTo(HaveOccurred())
				Expect(string(bytes)).To(Equal("stuff"))

				stream.Close()
				Expect(fakeStream.Closed()).To(BeTrue())
			})
		})

		Context("when the container doesn't exist", func() {
			BeforeEach(func() {
				fakeGardenClient.LookupReturns(nil, garden.ContainerNotFoundError{})
			})

			It("returns a container-not-found error", func() {
				_, err := gardenStore.GetFiles(logger, "the-guid", "the-path")
				Expect(err).To(Equal(executor.ErrContainerNotFound))
			})
		})
	})

	Describe("Ping", func() {
		Context("when pinging succeeds", func() {
			It("succeeds", func() {
				err := gardenStore.Ping()
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("when pinging fails", func() {
			disaster := errors.New("welp")

			BeforeEach(func() {
				fakeGardenClient.PingReturns(disaster)
			})

			It("returns a container-not-found error", func() {
				Expect(gardenStore.Ping()).To(Equal(disaster))
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
			runAction = &models.RunAction{User: "me", Path: "run"}
			monitorAction = &models.RunAction{User: "me", Path: "monitor"}

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
			Expect(err).NotTo(HaveOccurred())
			return result
		}

		Context("when the garden container lookup fails", func() {
			JustBeforeEach(func() {
				executorContainer, err = gardenStore.Create(logger, executorContainer)
				Expect(err).NotTo(HaveOccurred())

				gardenStore.Run(logger, executorContainer)
			})

			Context("when the lookup fails because the container is not found", func() {
				BeforeEach(func() {
					fakeGardenClient.LookupReturns(gardenContainer, garden.ContainerNotFoundError{"some-container-handle"})
				})

				It("logs that the container was not found", func() {
					Expect(logger).To(gbytes.Say(runSessionPrefix + "lookup-failed"))
					Expect(logger).To(gbytes.Say("some-container-handle"))
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
					Expect(logger).To(gbytes.Say(runSessionPrefix + "lookup-failed"))
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
				Expect(err).NotTo(HaveOccurred())

				err = gardenStore.Run(logger, executorContainer)
				Expect(err).NotTo(HaveOccurred())
			})

			It("transitions to running as soon as it starts running", func() {
				Eventually(containerStateGetter).Should(BeEquivalentTo(executor.StateRunning))
				Eventually(emitter.EmitCallCount).Should(Equal(1))
				Expect(emitter.EmitArgsForCall(0).EventType()).To(Equal(executor.EventTypeContainerRunning))
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
					Expect(emitter.EmitArgsForCall(1).EventType()).To(Equal(executor.EventTypeContainerComplete))
					Expect(containerResult().Failed).To(BeFalse())
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
					Expect(emitter.EmitArgsForCall(1).EventType()).To(Equal(executor.EventTypeContainerComplete))
					Expect(containerResult().Failed).To(BeTrue())
					Expect(containerResult().FailureReason).To(ContainSubstring("Exited with status 1"))
				})

				It("logs the unsuccessful exit", func() {
					Eventually(logger).Should(gbytes.Say(stepSessionPrefix + "step-finished-with-error"))
				})
			})
		})

		Context("when there is a monitor action", func() {
			JustBeforeEach(func() {
				executorContainer, err = gardenStore.Create(logger, executorContainer)
				Expect(err).NotTo(HaveOccurred())

				err = gardenStore.Run(logger, executorContainer)
				Expect(err).NotTo(HaveOccurred())

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
					Expect(emitter.EmitArgsForCall(0).EventType()).To(Equal(executor.EventTypeContainerRunning))
				})

				It("logs the run session lifecycle", func() {
					Expect(logger).To(gbytes.Say(runSessionPrefix + "started"))
					Expect(logger).To(gbytes.Say(runSessionPrefix + "found-garden-container"))
					Expect(logger).To(gbytes.Say(runSessionPrefix + "stored-step-process"))
					Expect(logger).To(gbytes.Say(runSessionPrefix + "finished"))
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
						Expect(emitter.EmitArgsForCall(1).EventType()).To(Equal(executor.EventTypeContainerComplete))
						Expect(containerResult().Failed).To(BeTrue())
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
						Expect(emitter.EmitArgsForCall(1).EventType()).To(Equal(executor.EventTypeContainerComplete))
						Expect(containerResult().Failed).To(BeFalse())
					})

					It("reports in the result that it was stopped", func() {
						Eventually(containerStateGetter).Should(BeEquivalentTo(executor.StateCompleted))
						Eventually(emitter.EmitCallCount).Should(Equal(2))
						Expect(emitter.EmitArgsForCall(1).EventType()).To(Equal(executor.EventTypeContainerComplete))
						Expect(containerResult().Stopped).To(BeTrue())
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
						Expect(emitter.EmitArgsForCall(1).EventType()).To(Equal(executor.EventTypeContainerComplete))
						Expect(containerResult().Failed).To(BeFalse())
					})

					It("reports in the result that it was stopped", func() {
						Eventually(containerStateGetter).Should(BeEquivalentTo(executor.StateCompleted))
						Eventually(emitter.EmitCallCount).Should(Equal(2))
						Expect(emitter.EmitArgsForCall(1).EventType()).To(Equal(executor.EventTypeContainerComplete))
						Expect(containerResult().Stopped).To(BeTrue())
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
						Expect(emitter.EmitArgsForCall(0).EventType()).To(Equal(executor.EventTypeContainerComplete))
						Expect(containerResult().Failed).To(BeTrue())
					})
				})
			})
		})

		Context("when marking the task as complete", func() {
			BeforeEach(func() {
				executorContainer, err = gardenStore.Create(logger, executorContainer)
				Expect(err).NotTo(HaveOccurred())

				err = gardenStore.Run(logger, executorContainer)
				Expect(err).NotTo(HaveOccurred())
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
				Expect(n).To(BeNumerically(">", 2))
				Expect(orderInWhichPropertiesAreSet[n-2]).To(Equal(gardenstore.ContainerResultProperty))
				Expect(orderInWhichPropertiesAreSet[n-1]).To(Equal(gardenstore.ContainerStateProperty))
			})
		})
	})

	Describe("Stop", func() {
		Context("when the garden container is not found in the local store", func() {
			var stopErr error

			JustBeforeEach(func() {
				stopErr = gardenStore.Stop(logger, "an-unknown-guid")
			})

			It("tries to destroy the real garden container", func() {
				Expect(fakeGardenClient.DestroyCallCount()).To(Equal(1))
				Expect(fakeGardenClient.DestroyArgsForCall(0)).To(Equal("an-unknown-guid"))
			})

			It("fails with container not found", func() {
				Expect(stopErr).To(Equal(executor.ErrContainerNotFound))
			})

			Context("when destroying the garden container fails", func() {
				Context("with a container not found", func() {
					BeforeEach(func() {
						fakeGardenClient.DestroyReturns(garden.ContainerNotFoundError{})
					})

					It("fails with executor's container not found error", func() {
						Expect(stopErr).To(Equal(executor.ErrContainerNotFound))
					})
				})

				Context("with any other error", func() {
					var expectedError = errors.New("woops")

					BeforeEach(func() {
						fakeGardenClient.DestroyReturns(expectedError)
					})

					It("fails with the original error", func() {
						Expect(stopErr).To(Equal(expectedError))
					})
				})
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
					TotalRss:              100, // ignored
					TotalCache:            12,  // ignored
					TotalInactiveFile:     1,   // ignored
					TotalUsageTowardLimit: 987,
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
			Expect(metricsErr).NotTo(HaveOccurred())
		})

		It("gets metrics from garden", func() {
			Expect(fakeGardenClient.BulkMetricsCallCount()).To(Equal(1))
			Expect(metrics).To(HaveLen(1))
			Expect(metrics["some-container-handle"]).To(Equal(executor.ContainerMetrics{
				MemoryUsageInBytes: 987,
				DiskUsageInBytes:   222,
				TimeSpentInCPU:     123,
			}))

		})

		Context("when a container metric entry has an error", func() {
			BeforeEach(func() {
				fakeGardenClient.BulkMetricsReturns(map[string]garden.ContainerMetricsEntry{
					"some-container-handle": garden.ContainerMetricsEntry{
						Err: garden.NewError("oh no"),
					},
				}, nil)
			})

			It("does not error", func() {
				Expect(metricsErr).NotTo(HaveOccurred())
			})

			It("ignores any container with errors", func() {
				Expect(fakeGardenClient.BulkMetricsCallCount()).To(Equal(1))
				Expect(metrics).To(HaveLen(0))
			})
		})

		Context("when a bulk metrics returns an error", func() {
			BeforeEach(func() {
				fakeGardenClient.BulkMetricsReturns(nil, errors.New("oh no"))
			})

			It("does not error", func() {
				Expect(metricsErr).To(HaveOccurred())
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
		Expect(err).To(HaveOccurred())
	case "does not occur":
		Expect(err).NotTo(HaveOccurred())
	default:
		Fail("unknown 'assertErr' expectation: " + expectation.assertError)
	}
}
