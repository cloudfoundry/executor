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

	It("can create containers from an Executor container, that convert back to the same thing", func() {
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

			State:           executor.StateCreated,
			ContainerHandle: "some-handle",
		}

		gardenContainer, err := exch.Executor2Garden(client, executorContainer)
		Ω(err).ShouldNot(HaveOccurred())

		Ω(exch.Garden2Executor(client, gardenContainer)).Should(Equal(executorContainer))
	})
})
