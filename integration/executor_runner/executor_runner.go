package executor_runner

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/cloudfoundry/gunk/runner_support"
	"github.com/onsi/ginkgo/config"
	. "github.com/onsi/gomega"
	"github.com/vito/cmdtest"
	. "github.com/vito/cmdtest/matchers"
)

type ExecutorRunner struct {
	executorBin   string
	wardenNetwork string
	wardenAddr    string
	etcdCluster   []string

	loggregatorServer string
	loggregatorSecret string

	Session *cmdtest.Session
	Config  Config
}

type Config struct {
	MemoryMB              int
	DiskMB                int
	ConvergenceInterval   time.Duration
	HeartbeatInterval     time.Duration
	Stack                 string
	TempDir               string
	TimeToClaim           time.Duration
	ContainerOwnerName    string
	ContainerMaxCpuShares int
	DrainTimeout          time.Duration
}

var defaultConfig = Config{
	MemoryMB:              1024,
	DiskMB:                1024,
	ConvergenceInterval:   30 * time.Second,
	HeartbeatInterval:     60 * time.Second,
	Stack:                 "lucid64",
	TempDir:               "/tmp",
	TimeToClaim:           30 * 60 * time.Second,
	ContainerOwnerName:    "",
	ContainerMaxCpuShares: 1024,
	DrainTimeout:          5 * time.Second,
}

func New(executorBin, wardenNetwork, wardenAddr string, etcdCluster []string, loggregatorServer string, loggregatorSecret string) *ExecutorRunner {
	return &ExecutorRunner{
		executorBin:   executorBin,
		wardenNetwork: wardenNetwork,
		wardenAddr:    wardenAddr,
		etcdCluster:   etcdCluster,

		loggregatorServer: loggregatorServer,
		loggregatorSecret: loggregatorSecret,

		Config: defaultConfig,
	}
}

func (r *ExecutorRunner) Start(config ...Config) {
	r.StartWithoutCheck(config...)

	Ω(r.Session).Should(SayWithTimeout("executor.started", 1*time.Second))
}

func (r *ExecutorRunner) StartWithoutCheck(config ...Config) {
	configToUse := r.generateConfig(config...)
	executorSession, err := cmdtest.StartWrapped(
		exec.Command(
			r.executorBin,
			"-wardenNetwork", r.wardenNetwork,
			"-wardenAddr", r.wardenAddr,
			"-etcdCluster", strings.Join(r.etcdCluster, ","),
			"-memoryMB", fmt.Sprintf("%d", configToUse.MemoryMB),
			"-diskMB", fmt.Sprintf("%d", configToUse.DiskMB),
			"-convergenceInterval", fmt.Sprintf("%s", configToUse.ConvergenceInterval),
			"-heartbeatInterval", fmt.Sprintf("%s", configToUse.HeartbeatInterval),
			"-stack", configToUse.Stack,
			"-loggregatorServer", r.loggregatorServer,
			"-loggregatorSecret", r.loggregatorSecret,
			"-tempDir", configToUse.TempDir,
			"-timeToClaimTask", fmt.Sprintf("%s", configToUse.TimeToClaim),
			"-containerOwnerName", configToUse.ContainerOwnerName,
			"-containerMaxCpuShares", fmt.Sprintf("%d", configToUse.ContainerMaxCpuShares),
			"-drainTimeout", fmt.Sprintf("%s", configToUse.DrainTimeout),
		),
		runner_support.TeeToGinkgoWriter,
		runner_support.TeeToGinkgoWriter,
	)
	Ω(err).ShouldNot(HaveOccurred())
	r.Config = configToUse
	r.Session = executorSession
}

func (r *ExecutorRunner) Stop() {
	r.Config = defaultConfig
	if r.Session != nil {
		r.Session.Cmd.Process.Signal(syscall.SIGTERM)
		_, err := r.Session.Wait(5 * time.Second)
		Ω(err).ShouldNot(HaveOccurred())
	}
}

func (r *ExecutorRunner) KillWithFire() {
	if r.Session != nil {
		r.Session.Cmd.Process.Kill()
	}
}

func (r *ExecutorRunner) generateConfig(configs ...Config) Config {
	configToReturn := defaultConfig
	configToReturn.ContainerOwnerName = fmt.Sprintf("executor-on-node-%d", config.GinkgoConfig.ParallelNode)

	if len(configs) == 0 {
		return configToReturn
	}

	givenConfig := configs[0]
	if givenConfig.MemoryMB != 0 {
		configToReturn.MemoryMB = givenConfig.MemoryMB
	}
	if givenConfig.DiskMB != 0 {
		configToReturn.DiskMB = givenConfig.DiskMB
	}
	if givenConfig.ConvergenceInterval != 0 {
		configToReturn.ConvergenceInterval = givenConfig.ConvergenceInterval
	}
	if givenConfig.HeartbeatInterval != 0 {
		configToReturn.HeartbeatInterval = givenConfig.HeartbeatInterval
	}
	if givenConfig.Stack != "" {
		configToReturn.Stack = givenConfig.Stack
	}
	if givenConfig.TempDir != "" {
		configToReturn.TempDir = givenConfig.TempDir
	}
	if givenConfig.TimeToClaim != 0 {
		configToReturn.TimeToClaim = givenConfig.TimeToClaim
	}
	if givenConfig.ContainerOwnerName != "" {
		configToReturn.ContainerOwnerName = givenConfig.ContainerOwnerName
	}

	return configToReturn
}
