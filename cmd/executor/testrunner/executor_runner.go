package testrunner

import (
	"os/exec"
	"strconv"
	"time"

	"github.com/tedsuo/ifrit/ginkgomon"
)

func New(
	executorBin,
	listenAddr,
	gardenNetwork,
	gardenAddr,
	loggregatorServer,
	loggregatorSecret,
	cachePath,
	tmpDir,
	debugAddr,
	containerOwnerName string,
	pruneInterval time.Duration,
	allowPrivileged bool,
) *ginkgomon.Runner {

	return ginkgomon.New(ginkgomon.Config{
		Name:          "executor",
		AnsiColorCode: "91m",
		StartCheck:    "executor.started",
		// executor may destroy containers on start, which can take a bit
		StartCheckTimeout: 30 * time.Second,
		Command: exec.Command(
			executorBin,
			"-listenAddr", listenAddr,
			"-gardenNetwork", gardenNetwork,
			"-gardenAddr", gardenAddr,
			"-loggregatorServer", loggregatorServer,
			"-loggregatorSecret", loggregatorSecret,
			"-containerMaxCpuShares", "1024",
			"-cachePath", cachePath,
			"-tempDir", tmpDir,
			"-containerOwnerName", containerOwnerName,
			"-containerInodeLimit", strconv.Itoa(245000),
			"-pruneInterval", pruneInterval.String(),
			"-debugAddr", debugAddr,
			"-gardenSyncInterval", "1s",
			"-allowPrivileged="+strconv.FormatBool(allowPrivileged),
			"-healthyMonitoringInterval", "1s",
			"-unhealthyMonitoringInterval", "100ms",
		),
	})
}
