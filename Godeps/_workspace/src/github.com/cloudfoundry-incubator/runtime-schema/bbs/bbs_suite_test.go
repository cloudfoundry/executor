package bbs_test

import (
	"os"
	"os/signal"
	"testing"
	"time"

	"github.com/cloudfoundry-incubator/runtime-schema/bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/bbs/fake_bbs"
	"github.com/cloudfoundry/storeadapter"
	"github.com/onsi/ginkgo/config"

	"github.com/cloudfoundry/storeadapter/storerunner/etcdstorerunner"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var etcdRunner *etcdstorerunner.ETCDClusterRunner
var etcdClient storeadapter.StoreAdapter

func TestBBS(t *testing.T) {
	RegisterFailHandler(Fail)

	etcdRunner = etcdstorerunner.NewETCDClusterRunner(5001+config.GinkgoConfig.ParallelNode, 1)

	registerSignalHandler()

	etcdRunner.Start()

	etcdClient = etcdRunner.Adapter()

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

func itRetriesUntilStoreComesBack(action func() error) {
	It("should keep trying until the store comes back", func(done Done) {
		etcdRunner.GoAway()

		runResult := make(chan error)
		go func() {
			err := action()
			runResult <- err
		}()

		time.Sleep(200 * time.Millisecond)

		etcdRunner.ComeBack()

		Ω(<-runResult).ShouldNot(HaveOccurred())

		close(done)
	}, 5)
}
