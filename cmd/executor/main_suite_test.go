// +build linux

// this has to be separate from the main_suite_test so we don't bother with
// this on non-linux, but still register the ginkgo handler for a better failure

package main_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	gardenrunner "github.com/cloudfoundry-incubator/garden-linux/integration/runner"
	garden "github.com/cloudfoundry-incubator/garden/api"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/ginkgomon"
)

var (
	gardenBinPath string
	helperRootfs  string

	builtComponents map[string]string

	gardenRunner  *gardenrunner.Runner
	gardenProcess ifrit.Process

	gardenAddr   string
	gardenClient garden.Client
)

var _ = SynchronizedBeforeSuite(func() []byte {
	executorBin, err := gexec.Build("github.com/cloudfoundry-incubator/executor/cmd/executor", "-race")
	Ω(err).ShouldNot(HaveOccurred())

	gardenLinuxBin, err := buildWithGodeps("github.com/cloudfoundry-incubator/garden-linux", "-race")
	Ω(err).ShouldNot(HaveOccurred())

	components, err := json.Marshal(map[string]string{
		"executor":     executorBin,
		"garden-linux": gardenLinuxBin,
	})
	Ω(err).ShouldNot(HaveOccurred())

	return components
}, func(components []byte) {
	err := json.Unmarshal(components, &builtComponents)
	Ω(err).ShouldNot(HaveOccurred())
})

var _ = SynchronizedAfterSuite(func() {
	//noop
}, func() {
	gexec.CleanupBuildArtifacts()
})

var _ = BeforeEach(func() {
	gardenBinPath = os.Getenv("GARDEN_BINPATH")
	Ω(gardenBinPath).ShouldNot(BeEmpty(), "must provide $GARDEN_BINPATH")

	gardenTestRootfs := os.Getenv("GARDEN_TEST_ROOTFS")
	Ω(gardenTestRootfs).ShouldNot(BeEmpty(), "must provide $GARDEN_TEST_ROOTFS")

	gardenAddr = fmt.Sprintf("127.0.0.1:%d", 7777+GinkgoParallelNode())

	gardenRunner = gardenrunner.New(
		"tcp",
		gardenAddr,
		builtComponents["garden-linux"],
		gardenBinPath,
		gardenTestRootfs,
		"/tmp",
	)

	gardenClient = gardenRunner.NewClient()

	gardenProcess = ifrit.Invoke(gardenRunner)
})

var _ = AfterEach(func() {
	containers, err := gardenClient.Containers(nil)
	Ω(err).ShouldNot(HaveOccurred())

	for _, container := range containers {
		err := gardenClient.Destroy(container.Handle())
		Ω(err).ShouldNot(HaveOccurred())
	}

	ginkgomon.Interrupt(gardenProcess)
})

func buildWithGodeps(pkg string, args ...string) (string, error) {
	gopath := fmt.Sprintf(
		"%s%c%s",
		filepath.Join(os.Getenv("GOPATH"), "src", pkg, "Godeps", "_workspace"),
		os.PathListSeparator,
		os.Getenv("GOPATH"),
	)

	return gexec.BuildIn(gopath, pkg, args...)
}
