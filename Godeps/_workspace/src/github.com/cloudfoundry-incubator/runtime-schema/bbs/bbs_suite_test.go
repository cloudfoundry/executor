package bbs_test

import (
	"github.com/cloudfoundry/storeadapter"
	"github.com/onsi/ginkgo/config"
	"os"
	"os/signal"
	"testing"

	"github.com/cloudfoundry/storeadapter/storerunner/etcdstorerunner"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var etcdRunner *etcdstorerunner.ETCDClusterRunner
var store storeadapter.StoreAdapter

func TestBBS(t *testing.T) {
	registerSignalHandler()
	RegisterFailHandler(Fail)

	etcdRunner = etcdstorerunner.NewETCDClusterRunner(5001+config.GinkgoConfig.ParallelNode, 1)
	etcdRunner.Start()

	store = etcdRunner.Adapter()

	RunSpecs(t, "BBS Suite")

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
