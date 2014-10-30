package exchanger_test

import (
	"encoding/json"
	"errors"

	"github.com/cloudfoundry-incubator/executor"
	. "github.com/cloudfoundry-incubator/executor/depot/exchanger"
	"github.com/cloudfoundry-incubator/executor/depot/exchanger/fakes"
	garden "github.com/cloudfoundry-incubator/garden/api"
	gfakes "github.com/cloudfoundry-incubator/garden/api/fakes"
	"github.com/cloudfoundry-incubator/runtime-schema/models"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Exchanger", func() {
	var (
		ownerName           = "some-owner-name"
		maxCPUShares uint64 = 1024
		inodeLimit   uint64 = 2000000

		exchanger Exchanger

		gardenClient *fakes.FakeGardenClient
	)

	BeforeEach(func() {
		exchanger = NewExchanger(ownerName, maxCPUShares, inodeLimit)
	})

	Describe("Garden2Executor", func() {
		var (
			gardenContainer *gfakes.FakeContainer

			executorContainer executor.Container
			exchangeErr       error
		)

		BeforeEach(func() {
			gardenContainer = new(gfakes.FakeContainer)
			gardenContainer.HandleReturns("some-container-handle")
		})

		JustBeforeEach(func() {
			executorContainer, exchangeErr = exchanger.Garden2Executor(gardenContainer)
		})

		It("does not error", func() {
			Ω(exchangeErr).ShouldNot(HaveOccurred())
		})

		Describe("the exchanged Executor container", func() {
			It("has the Garden container handle as its container guid", func() {
				Ω(executorContainer.Guid).Should(Equal("some-container-handle"))
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
						Ω(exchangeErr).Should(Equal(InvalidStateError{"bogus-state"}))
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

					It("returns an InvalidTimestampError", func() {
						Ω(exchangeErr).Should(Equal(InvalidTimestampError{"some-bogus-timestamp"}))
					})
				})
			})

			Context("when the container has an executor:rootfs property", func() {
				BeforeEach(func() {
					gardenContainer.InfoReturns(garden.ContainerInfo{
						Properties: garden.Properties{
							"executor:rootfs": "focker:///some-rootfs",
						},
					}, nil)
				})

				It("has it as its rootfs path", func() {
					Ω(executorContainer.RootFSPath).Should(Equal("focker:///some-rootfs"))
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
						Ω(exchangeErr).Should(HaveOccurred())
						Ω(exchangeErr.Error()).Should(ContainSubstring("executor:actions"))
						Ω(exchangeErr.Error()).Should(ContainSubstring("ß"))
						Ω(exchangeErr.Error()).Should(ContainSubstring("invalid character"))
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
						Ω(exchangeErr).Should(HaveOccurred())
						Ω(exchangeErr.Error()).Should(ContainSubstring("executor:env"))
						Ω(exchangeErr.Error()).Should(ContainSubstring("ß"))
						Ω(exchangeErr.Error()).Should(ContainSubstring("invalid character"))
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
						Ω(exchangeErr).Should(HaveOccurred())
						Ω(exchangeErr.Error()).Should(ContainSubstring("executor:log"))
						Ω(exchangeErr.Error()).Should(ContainSubstring("ß"))
						Ω(exchangeErr.Error()).Should(ContainSubstring("invalid character"))
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
						Ω(exchangeErr).Should(HaveOccurred())
						Ω(exchangeErr.Error()).Should(ContainSubstring("executor:result"))
						Ω(exchangeErr.Error()).Should(ContainSubstring("ß"))
						Ω(exchangeErr.Error()).Should(ContainSubstring("invalid character"))
					})
				})
			})

			Context("when the container has memory limits", func() {
				BeforeEach(func() {
					gardenContainer.CurrentMemoryLimitsReturns(garden.MemoryLimits{
						LimitInBytes: 64 * 1024 * 1024,
					}, nil)
				})

				It("returns them", func() {
					Ω(executorContainer.MemoryMB).Should(Equal(64))
				})
			})

			Context("when the container has disk limits", func() {
				BeforeEach(func() {
					gardenContainer.CurrentDiskLimitsReturns(garden.DiskLimits{
						ByteHard: 64 * 1024 * 1024,
					}, nil)
				})

				It("returns them", func() {
					Ω(executorContainer.DiskMB).Should(Equal(64))
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
					Ω(exchangeErr).Should(Equal(disaster))
				})
			})

			Context("when getting the current memory limits from Garden fails", func() {
				disaster := errors.New("oh no!")

				BeforeEach(func() {
					gardenContainer.CurrentMemoryLimitsReturns(garden.MemoryLimits{}, disaster)
				})

				It("returns the error", func() {
					Ω(exchangeErr).Should(Equal(disaster))
				})
			})

			Context("when getting the current disk limits from Garden fails", func() {
				disaster := errors.New("oh no!")

				BeforeEach(func() {
					gardenContainer.CurrentDiskLimitsReturns(garden.DiskLimits{}, disaster)
				})

				It("returns the error", func() {
					Ω(exchangeErr).Should(Equal(disaster))
				})
			})

			Context("when getting the current CPU limits from Garden fails", func() {
				disaster := errors.New("oh no!")

				BeforeEach(func() {
					gardenContainer.CurrentCPULimitsReturns(garden.CPULimits{}, disaster)
				})

				It("returns the error", func() {
					Ω(exchangeErr).Should(Equal(disaster))
				})
			})
		})
	})

	Describe("Executor2Garden", func() {
		var (
			executorContainer   executor.Container
			fakeGardenContainer *gfakes.FakeContainer

			gardenContainer garden.Container
			exchangeErr     error
		)

		BeforeEach(func() {
			executorContainer = executor.Container{
				Guid: "some-guid",
			}

			gardenClient = new(fakes.FakeGardenClient)
			fakeGardenContainer = new(gfakes.FakeContainer)
		})

		JustBeforeEach(func() {
			gardenContainer, exchangeErr = exchanger.Executor2Garden(gardenClient, executorContainer)
		})

		Context("when creating the container succeeds", func() {
			BeforeEach(func() {
				gardenClient.CreateReturns(fakeGardenContainer, nil)
			})

			It("does not error", func() {
				Ω(exchangeErr).ShouldNot(HaveOccurred())
			})

			Describe("the exchanged Garden container", func() {
				It("creates it with the owner property", func() {
					containerSpec := gardenClient.CreateArgsForCall(0)
					Ω(containerSpec.Properties["executor:owner"]).Should(Equal(ownerName))
				})

				It("creates it with the guid as the handle", func() {
					containerSpec := gardenClient.CreateArgsForCall(0)
					Ω(containerSpec.Handle).Should(Equal("some-guid"))
				})

				Context("when the Executor container has a rootfs", func() {
					BeforeEach(func() {
						executorContainer.RootFSPath = "focker:///some-rootfs"
					})

					It("creates it with the rootfs", func() {
						containerSpec := gardenClient.CreateArgsForCall(0)
						Ω(containerSpec.RootFSPath).Should(Equal("focker:///some-rootfs"))
					})
				})

				Context("when the Executor container has a state", func() {
					BeforeEach(func() {
						executorContainer.State = executor.StateReserved
					})

					It("creates it with the executor:state property", func() {
						containerSpec := gardenClient.CreateArgsForCall(0)
						Ω(containerSpec.Properties["executor:state"]).To(Equal(string(executor.StateReserved)))
					})
				})

				Context("when the Executor container an allocated at time", func() {
					BeforeEach(func() {
						executorContainer.AllocatedAt = 123456789
					})

					It("creates it with the executor:allocated-at property", func() {
						containerSpec := gardenClient.CreateArgsForCall(0)
						Ω(containerSpec.Properties["executor:allocated-at"]).To(Equal("123456789"))
					})
				})

				Context("when the Executor container has a root fs", func() {
					BeforeEach(func() {
						executorContainer.RootFSPath = "some/root/path"
					})

					It("creates it with the executor:rootfs property", func() {
						containerSpec := gardenClient.CreateArgsForCall(0)
						Ω(containerSpec.Properties["executor:rootfs"]).To(Equal("some/root/path"))
					})
				})

				Context("when the Executor container has a complete-url", func() {
					BeforeEach(func() {
						executorContainer.CompleteURL = "http://callback.example.com/bye"
					})

					It("creates it with the executor:complete-url property", func() {
						containerSpec := gardenClient.CreateArgsForCall(0)
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

						containerSpec := gardenClient.CreateArgsForCall(0)
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

						containerSpec := gardenClient.CreateArgsForCall(0)
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

						containerSpec := gardenClient.CreateArgsForCall(0)
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

						containerSpec := gardenClient.CreateArgsForCall(0)
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
					containerSpec := gardenClient.CreateArgsForCall(0)
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
						Ω(exchangeErr).Should(Equal(disaster))
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

				Context("and limiting memory fails", func() {
					disaster := errors.New("oh no!")

					BeforeEach(func() {
						fakeGardenContainer.LimitMemoryReturns(disaster)
					})

					It("returns the error", func() {
						Ω(exchangeErr).Should(Equal(disaster))
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

				Context("and limiting disk fails", func() {
					disaster := errors.New("oh no!")

					BeforeEach(func() {
						fakeGardenContainer.LimitDiskReturns(disaster)
					})

					It("returns the error", func() {
						Ω(exchangeErr).Should(Equal(disaster))
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
				gardenClient.CreateReturns(nil, disaster)
			})

			It("returns the error", func() {
				Ω(exchangeErr).Should(Equal(disaster))
			})
		})
	})
})
