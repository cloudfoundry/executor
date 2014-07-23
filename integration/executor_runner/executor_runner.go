package executor_runner

import (
	"fmt"
	"os/exec"
	"time"

	"github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/config"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/gexec"
)

type ExecutorRunner struct {
	executorBin   string
	listenAddr    string
	wardenNetwork string
	wardenAddr    string

	loggregatorServer string
	loggregatorSecret string

	Session *gexec.Session
	Config  Config
}

type Config struct {
	MemoryMB                string
	DiskMB                  string
	TempDir                 string
	ContainerOwnerName      string
	ContainerMaxCpuShares   int
	RegistryPruningInterval time.Duration
}

var defaultConfig = Config{
	MemoryMB:                "1024",
	DiskMB:                  "1024",
	TempDir:                 "/tmp",
	ContainerOwnerName:      "",
	ContainerMaxCpuShares:   1024,
	RegistryPruningInterval: time.Minute,
}

func New(executorBin, listenAddr, wardenNetwork, wardenAddr string, loggregatorServer string, loggregatorSecret string) *ExecutorRunner {
	return &ExecutorRunner{
		executorBin:   executorBin,
		listenAddr:    listenAddr,
		wardenNetwork: wardenNetwork,
		wardenAddr:    wardenAddr,

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
	if r.Session != nil {
		panic("starting more than one executor!!!")
	}

	configToUse := r.generateConfig(config...)
	executorSession, err := gexec.Start(
		exec.Command(
			r.executorBin,
			"-listenAddr", r.listenAddr,
			"-wardenNetwork", r.wardenNetwork,
			"-wardenAddr", r.wardenAddr,
			"-memoryMB", configToUse.MemoryMB,
			"-diskMB", configToUse.DiskMB,
			"-loggregatorServer", r.loggregatorServer,
			"-loggregatorSecret", r.loggregatorSecret,
			"-tempDir", configToUse.TempDir,
			"-containerOwnerName", configToUse.ContainerOwnerName,
			"-containerMaxCpuShares", fmt.Sprintf("%d", configToUse.ContainerMaxCpuShares),
			"-pruneInterval", fmt.Sprintf("%s", configToUse.RegistryPruningInterval),
		),
		gexec.NewPrefixedWriter("\x1b[32m[o]\x1b[36m[executor]\x1b[0m ", ginkgo.GinkgoWriter),
		gexec.NewPrefixedWriter("\x1b[91m[e]\x1b[36m[executor]\x1b[0m ", ginkgo.GinkgoWriter),
	)
	Ω(err).ShouldNot(HaveOccurred())
	r.Config = configToUse
	r.Session = executorSession
}

func (r *ExecutorRunner) Stop() {
	r.Config = defaultConfig
	if r.Session != nil {
		r.Session.Terminate().Wait(5 * time.Second)
		r.Session = nil
	}
}

func (r *ExecutorRunner) KillWithFire() {
	if r.Session != nil {
		r.Session.Kill().Wait(5 * time.Second)
		r.Session = nil
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
	if givenConfig.TempDir != "" {
		configToReturn.TempDir = givenConfig.TempDir
	}
	if givenConfig.ContainerOwnerName != "" {
		configToReturn.ContainerOwnerName = givenConfig.ContainerOwnerName
	}
	if givenConfig.ContainerMaxCpuShares != 0 {
		configToReturn.ContainerMaxCpuShares = givenConfig.ContainerMaxCpuShares
	}
	if givenConfig.RegistryPruningInterval != 0 {
		configToReturn.RegistryPruningInterval = givenConfig.RegistryPruningInterval
	}

	return configToReturn
}
