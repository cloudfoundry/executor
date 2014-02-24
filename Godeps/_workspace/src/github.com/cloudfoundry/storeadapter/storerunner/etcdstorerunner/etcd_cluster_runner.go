package etcdstorerunner

import (
	"fmt"
	"github.com/cloudfoundry/storeadapter"
	"github.com/cloudfoundry/storeadapter/etcdstoreadapter"
	"github.com/cloudfoundry/storeadapter/workerpool"
	. "github.com/onsi/gomega"
	"log"

	etcdclient "github.com/coreos/go-etcd/etcd"

	"os"
	"os/exec"
	"syscall"
)

type ETCDClusterRunner struct {
	startingPort int
	numNodes     int
	etcdCommands []*exec.Cmd
	running      bool
	client       *etcdclient.Client
}

func NewETCDClusterRunner(startingPort int, numNodes int) *ETCDClusterRunner {
	return &ETCDClusterRunner{
		startingPort: startingPort,
		numNodes:     numNodes,
	}
}

func (etcd *ETCDClusterRunner) Start() {
	etcd.start(true)
}

func (etcd *ETCDClusterRunner) Stop() {
	etcd.stop(true)
}

func (etcd *ETCDClusterRunner) GoAway() {
	etcd.stop(false)
}

func (etcd *ETCDClusterRunner) ComeBack() {
	etcd.start(false)
}

func (etcd *ETCDClusterRunner) NodeURLS() []string {
	urls := make([]string, etcd.numNodes)
	for i := 0; i < etcd.numNodes; i++ {
		urls[i] = "http://" + etcd.clientUrl(i)
	}
	return urls
}

func (etcd *ETCDClusterRunner) DiskUsage() (bytes int64, err error) {
	fi, err := os.Stat(etcd.tmpPathTo("log", 0))
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

func (etcd *ETCDClusterRunner) Reset() {
	if etcd.running {
		response, err := etcd.client.Get("/", false, false)
		Ω(err).ShouldNot(HaveOccurred())
		for _, doomed := range response.Node.Nodes {
			etcd.client.Delete(doomed.Key, true)
		}
	}
}

func (etcd *ETCDClusterRunner) FastForwardTime(seconds int) {
	if etcd.running {
		response, err := etcd.client.Get("/", false, true)
		Ω(err).ShouldNot(HaveOccurred())
		etcd.fastForwardTime(*response.Node, seconds)
	}
}

func (etcd *ETCDClusterRunner) Adapter() storeadapter.StoreAdapter {
	pool := workerpool.NewWorkerPool(10)
	adapter := etcdstoreadapter.NewETCDStoreAdapter(etcd.NodeURLS(), pool)
	adapter.Connect()
	return adapter
}

func (etcd *ETCDClusterRunner) start(nuke bool) {
	if etcd.running {
		return
	}

	etcd.etcdCommands = make([]*exec.Cmd, etcd.numNodes)

	for i := 0; i < etcd.numNodes; i++ {
		if nuke {
			etcd.nukeArtifacts(i)
		}

		if etcd.detectRunningEtcd(i) {
			log.Fatalf("Detected an ETCD already running on %s", etcd.clientUrl(i))
		}

		os.MkdirAll(etcd.tmpPath(i), 0700)
		args := []string{"-data-dir", etcd.tmpPath(i), "-addr", etcd.clientUrl(i), "-peer-addr", etcd.serverUrl(i), "-name", etcd.nodeName(i)}
		if i != 0 {
			args = append(args, "-peers", etcd.serverUrl(0))
		}

		etcd.etcdCommands[i] = exec.Command("etcd", args...)

		err := etcd.etcdCommands[i].Start()
		Ω(err).ShouldNot(HaveOccurred(), "Make sure etcd is compiled and on your $PATH.")

		Eventually(func() bool {
			return etcd.detectRunningEtcd(i)
		}, 3, 0.05).Should(BeTrue(), "Expected ETCD to be up and running")
	}

	etcd.client = etcdclient.NewClient(etcd.NodeURLS())
	etcd.running = true
}

func (etcd *ETCDClusterRunner) stop(nuke bool) {
	if etcd.running {
		for i := 0; i < etcd.numNodes; i++ {
			etcd.etcdCommands[i].Process.Signal(syscall.SIGINT)
			etcd.etcdCommands[i].Process.Wait()
			if nuke {
				etcd.nukeArtifacts(i)
			}
		}
		etcd.etcdCommands = nil
		etcd.running = false
		etcd.client = nil
	}
}

func (etcd *ETCDClusterRunner) detectRunningEtcd(index int) bool {
	client := etcdclient.NewClient([]string{})
	return client.SetCluster([]string{"http://" + etcd.clientUrl(index)})
}

func (etcd *ETCDClusterRunner) fastForwardTime(etcdNode etcdclient.Node, seconds int) {
	if etcdNode.Dir == true {
		for _, child := range etcdNode.Nodes {
			etcd.fastForwardTime(child, seconds)
		}
	} else {
		if etcdNode.TTL == 0 {
			return
		}
		if etcdNode.TTL <= int64(seconds) {
			_, err := etcd.client.Delete(etcdNode.Key, true)
			Ω(err).ShouldNot(HaveOccurred())
		} else {
			_, err := etcd.client.Set(etcdNode.Key, etcdNode.Value, uint64(etcdNode.TTL-int64(seconds)))
			Ω(err).ShouldNot(HaveOccurred())
		}
	}
}

func (etcd *ETCDClusterRunner) clientUrl(index int) string {
	return fmt.Sprintf("127.0.0.1:%d", etcd.port(index))
}

func (etcd *ETCDClusterRunner) serverUrl(index int) string {
	return fmt.Sprintf("127.0.0.1:%d", etcd.port(index)+3000)
}

func (etcd *ETCDClusterRunner) nodeName(index int) string {
	return fmt.Sprintf("node%d", index)
}

func (etcd *ETCDClusterRunner) port(index int) int {
	return etcd.startingPort + index
}

func (etcd *ETCDClusterRunner) tmpPath(index int) string {
	return fmt.Sprintf("/tmp/ETCD_%d", etcd.port(index))
}

func (etcd *ETCDClusterRunner) tmpPathTo(subdir string, index int) string {
	return fmt.Sprintf("/%s/%s", etcd.tmpPath(index), subdir)
}

func (etcd *ETCDClusterRunner) nukeArtifacts(index int) {
	os.RemoveAll(etcd.tmpPath(index))
}
