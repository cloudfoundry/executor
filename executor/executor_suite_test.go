package executor_test

import (
	"github.com/onsi/ginkgo/config"
	"os"
	"os/signal"
	"testing"

	"github.com/cloudfoundry/storeadapter/storerunner"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var etcdRunner *storerunner.ETCDClusterRunner

func TestExecutor(t *testing.T) {
	registerSignalHandler()
	RegisterFailHandler(Fail)

	etcdRunner = storerunner.NewETCDClusterRunner(5001+config.GinkgoConfig.ParallelNode, 1)
	etcdRunner.Start()

	RunSpecs(t, "Executor Suite")

	etcdRunner.Stop()
}

var _ = BeforeEach(func() {
	etcdRunner.Stop()
	etcdRunner.Start()
})

func registerSignalHandler() {
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, os.Kill)

		select {
		case <-c:
			etcdRunner.Stop()
			os.Exit(0)
		}
	}()
}
