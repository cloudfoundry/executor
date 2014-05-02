package executor_runner

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/config"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/gexec"
)

type ExecutorRunner struct {
	executorBin   string
	wardenNetwork string
	wardenAddr    string
	etcdCluster   []string

	loggregatorServer string
	loggregatorSecret string

	Session *gexec.Session
	Config  Config
}

type Config struct {
	MemoryMB              string
	DiskMB                string
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
	MemoryMB:              "1024",
	DiskMB:                "1024",
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

	Eventually(r.Session, 5*time.Second).Should(gbytes.Say("executor.started"))
}

func (r *ExecutorRunner) StartWithoutCheck(config ...Config) {
	configToUse := r.generateConfig(config...)
	executorSession, err := gexec.Start(
		exec.Command(
			r.executorBin,
			"-wardenNetwork", r.wardenNetwork,
			"-wardenAddr", r.wardenAddr,
			"-etcdCluster", strings.Join(r.etcdCluster, ","),
			"-memoryMB", configToUse.MemoryMB,
			"-diskMB", configToUse.DiskMB,
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
		ginkgo.GinkgoWriter,
		ginkgo.GinkgoWriter,
	)
	Ω(err).ShouldNot(HaveOccurred())
	r.Config = configToUse
	r.Session = executorSession
}

func (r *ExecutorRunner) Stop() {
	r.Config = defaultConfig
	if r.Session != nil {
		r.Session.Terminate().Wait(5 * time.Second)
	}
}

func (r *ExecutorRunner) KillWithFire() {
	if r.Session != nil {
		r.Session.Kill().Wait(5 * time.Second)
	}
}

func (r *ExecutorRunner) generateConfig(configs ...Config) Config {
	configToReturn := defaultConfig
	configToReturn.ContainerOwnerName = fmt.Sprintf("executor-on-node-%d", config.GinkgoConfig.ParallelNode)

	if len(configs) == 0 {
		return configToReturn
	}

	givenConfig := configs[0]
	if givenConfig.MemoryMB != "" {
		configToReturn.MemoryMB = givenConfig.MemoryMB
	}
	if givenConfig.DiskMB != "" {
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
