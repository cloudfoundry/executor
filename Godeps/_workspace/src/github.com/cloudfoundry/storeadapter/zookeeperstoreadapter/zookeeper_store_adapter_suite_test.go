package zookeeperstoreadapter_test

import (
	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/config"
	. "github.com/onsi/gomega"

	"github.com/cloudfoundry/storeadapter/storerunner/zookeeperstorerunner"

	"os"
	"os/signal"
	"testing"
)

var zookeeperRunner *zookeeperstorerunner.ZookeeperClusterRunner

func TestStoreAdapter(t *testing.T) {
	registerSignalHandler()
	RegisterFailHandler(Fail)

	zookeeperPort := 2181 + (config.GinkgoConfig.ParallelNode-1)*10
	zookeeperRunner = zookeeperstorerunner.NewZookeeperClusterRunner(zookeeperPort, 1)

	zookeeperRunner.Start()

	RunSpecs(t, "ZooKeeper Store Adapter Suite")

	stopStores()
}

var _ = BeforeEach(func() {
	zookeeperRunner.Reset()
})

func stopStores() {
	zookeeperRunner.Stop()
}

func registerSignalHandler() {
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, os.Kill)

		select {
		case <-c:
			stopStores()
			os.Exit(0)
		}
	}()
}
