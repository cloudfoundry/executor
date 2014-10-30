package allocations_test

import (
	"github.com/cloudfoundry-incubator/executor"
	. "github.com/cloudfoundry-incubator/executor/depot/allocations"
	"github.com/cloudfoundry-incubator/executor/depot/exchanger"
	garden "github.com/cloudfoundry-incubator/garden/api"
	"github.com/cloudfoundry-incubator/runtime-schema/models"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Client", func() {
	var (
		client garden.Client

		exch exchanger.Exchanger
	)

	BeforeEach(func() {
		client = NewClient()

		exch = exchanger.NewExchanger("some-owner", 1024, 20000)
	})

	Describe("Create", func() {
		logIndex := 1

		executorContainer := executor.Container{
			Guid: "some-guid",

			MemoryMB:  1024,
			DiskMB:    512,
			CPUWeight: 50,

			Tags: executor.Tags{"a": "1", "b": "2"},

			AllocatedAt: 1234,

			RootFSPath: "some-rootfs",
			Ports: []executor.PortMapping{
				{HostPort: 1234, ContainerPort: 5678},
			},

			Log: executor.LogConfig{
				Guid:       "some-guid",
				SourceName: "some-source",
				Index:      &logIndex,
			},

			Actions: []models.ExecutorAction{
				{
					models.RunAction{
						Path: "ls",
					},
				},
			},

			Env: []executor.EnvironmentVariable{{Name: "FOO", Value: "bar"}},

			CompleteURL: "some-complete-url",

			RunResult: executor.ContainerRunResult{
				Failed:        true,
				FailureReason: "because",
			},

			State: executor.StateCreated,
		}

		It("can create containers from an Executor container, that convert back to the same thing", func() {
			gardenContainer, err := exch.Executor2Garden(client, executorContainer)
			Ω(err).ShouldNot(HaveOccurred())
			Ω(exch.Garden2Executor(gardenContainer)).Should(Equal(executorContainer))
		})

		It("makes the container appear on subsequent reads", func() {
			gardenContainer, err := exch.Executor2Garden(client, executorContainer)
			Ω(err).ShouldNot(HaveOccurred())
			Ω(client.Containers(garden.Properties{})).Should(Equal([]garden.Container{gardenContainer}))
		})

		It("makes the container appear on subsequent lookups", func() {
			gardenContainerA, err := client.Create(garden.ContainerSpec{Handle: "a"})
			Ω(err).ShouldNot(HaveOccurred())

			gardenContainerB, err := client.Create(garden.ContainerSpec{Handle: "b"})
			Ω(err).ShouldNot(HaveOccurred())

			Ω(client.Lookup("a")).Should(Equal(gardenContainerA))
			Ω(client.Lookup("b")).Should(Equal(gardenContainerB))
		})
	})

	Describe("Lookup", func() {
		Context("when a container does not exist for the handle", func() {
			It("returns an error", func() {
				_, err := client.Lookup("bogus")
				Ω(err).Should(HaveOccurred())
			})
		})
	})

	Describe("Destroy", func() {
		Context("when a container exists for the handle", func() {
			BeforeEach(func() {
				_, err := client.Create(garden.ContainerSpec{Handle: "some-handle"})
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("makes the container not appear on subsequent lookups", func() {
				_, err := client.Lookup("some-handle")
				Ω(err).ShouldNot(HaveOccurred())

				err = client.Destroy("some-handle")
				Ω(err).ShouldNot(HaveOccurred())

				_, err = client.Lookup("some-handle")
				Ω(err).Should(HaveOccurred())
			})
		})

		Context("when a container does not exist for the handle", func() {
			It("returns an error", func() {
				err := client.Destroy("bogus")
				Ω(err).Should(HaveOccurred())
			})
		})
	})

	Describe("Containers", func() {
		It("filters by property", func() {
			a, err := client.Create(garden.ContainerSpec{
				Handle: "some-handle-a",
				Properties: garden.Properties{
					"a": "a-value",
				},
			})
			Ω(err).ShouldNot(HaveOccurred())

			b, err := client.Create(garden.ContainerSpec{
				Handle: "some-handle-b",
				Properties: garden.Properties{
					"a": "a-value",
					"x": "anded-value",
				},
			})
			Ω(err).ShouldNot(HaveOccurred())

			c, err := client.Create(garden.ContainerSpec{
				Handle: "some-handle-c",
				Properties: garden.Properties{
					"x": "anded-value",
				},
			})
			Ω(err).ShouldNot(HaveOccurred())

			Ω(client.Containers(garden.Properties{
				"a": "a-value",
			})).Should(ConsistOf([]garden.Container{a, b}))

			Ω(client.Containers(garden.Properties{
				"a": "a-value",
				"x": "anded-value",
			})).Should(ConsistOf([]garden.Container{b}))

			Ω(client.Containers(garden.Properties{
				"x": "anded-value",
			})).Should(ConsistOf([]garden.Container{b, c}))
		})
	})
})
