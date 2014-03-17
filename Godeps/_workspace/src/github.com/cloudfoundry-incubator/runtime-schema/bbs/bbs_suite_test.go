package bbs_test

import (
	"github.com/cloudfoundry-incubator/runtime-schema/bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/bbs/fake_bbs"
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

var _ = It("should have a valid fake", func() {
	var fakeExecutorBBS bbs.ExecutorBBS
	fakeExecutorBBS = fake_bbs.NewFakeExecutorBBS()
	Ω(fakeExecutorBBS).ShouldNot(BeNil())

	var fakeStagerBBS bbs.StagerBBS
	fakeStagerBBS = fake_bbs.NewFakeStagerBBS()
	Ω(fakeStagerBBS).ShouldNot(BeNil())
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
