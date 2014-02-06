package zookeeperstorerunner

import (
	"errors"
	"github.com/cloudfoundry/storeadapter"
	. "github.com/onsi/gomega"
	"time"

	"fmt"
	zkClient "github.com/samuel/go-zookeeper/zk"
	"io/ioutil"
	"os"
	"os/exec"
)

//See http://zookeeper.apache.org/doc/trunk/zookeeperStarted.html

type ZookeeperClusterRunner struct {
	startingPort int
	numNodes     int
	running      bool
}

func NewZookeeperClusterRunner(startingPort int, numNodes int) *ZookeeperClusterRunner {
	return &ZookeeperClusterRunner{
		startingPort: startingPort,
		numNodes:     numNodes,
	}
}

func (zk *ZookeeperClusterRunner) Start() {
	zk.start(true)
}

func (zk *ZookeeperClusterRunner) Stop() {
	zk.stop(true)
}

func (zk *ZookeeperClusterRunner) GoAway() {
	zk.stop(false)
}

func (zk *ZookeeperClusterRunner) ComeBack() {
	zk.start(false)
}

func (zk *ZookeeperClusterRunner) NodeURLS() []string {
	urls := make([]string, zk.numNodes)
	for i := 0; i < zk.numNodes; i++ {
		urls[i] = zk.clientUrl(i)
	}
	return urls
}

func (zk *ZookeeperClusterRunner) DiskUsage() (bytes int64, err error) {
	return 0, nil
	fi, err := os.Stat(zk.tmpPathTo("version-2/snapshot.0", 0))
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

func (zk *ZookeeperClusterRunner) Reset() {
	client, _, err := zkClient.Connect(zk.NodeURLS(), time.Second)
	Ω(err).ShouldNot(HaveOccurred(), "Failed to connect")

	zk.deleteRecursively(client, "/")
	client.Close()
}

func (zk *ZookeeperClusterRunner) Adapter() storeadapter.StoreAdapter {
	panic("go make your own!")
	return nil
}

func (zk *ZookeeperClusterRunner) deleteRecursively(client *zkClient.Conn, key string) {
	if key != "/zookeeper" {
		children, _, err := client.Children(key)
		Ω(err).ShouldNot(HaveOccurred(), "Failed to fetch children for '%s'", key)

		for _, child := range children {
			childPath := key + "/" + child
			if key == "/" {
				childPath = key + child
			}
			zk.deleteRecursively(client, childPath)
		}

		if key != "/" {
			err = client.Delete(key, -1)
		}

		Ω(err).ShouldNot(HaveOccurred(), "Failed to delete key '%s'", key)
	}

	return
}

func (zk *ZookeeperClusterRunner) FastForwardTime(seconds int) {
	//nothing to do here.  FastForwardTime is only present to fast-forward TTLs.
	//Since TTLs are implemented by the driver, and time is always injected in, we are fine.
}

func (zk *ZookeeperClusterRunner) zkExecutable() (string, error) {
	path, err := exec.LookPath("zkServer.sh")
	if err == nil {
		return path, nil
	}
	path, err = exec.LookPath("zkServer")
	if err == nil {
		return path, nil
	}

	return "", errors.New("Make sure ZooKeeper is compiled and on your $PATH")
}

func (zk *ZookeeperClusterRunner) start(nuke bool) {
	for i := 0; i < zk.numNodes; i++ {
		if nuke {
			zk.nukeArtifacts(i)
		}
		os.MkdirAll(zk.tmpPath(i), 0700)
		zk.writeId(i)
		zk.writeConfig(i)

		zkExecutable, err := zk.zkExecutable()
		Ω(err).ShouldNot(HaveOccurred())

		cmd := exec.Command(zkExecutable, "start", zk.configPath(i))
		cmd.Env = append(os.Environ(), "ZOO_LOG_DIR="+zk.tmpPath(i))

		out, err := cmd.Output()
		Ω(err).ShouldNot(HaveOccurred(), "Make sure zookeeper is compiled and on your $PATH.")
		Ω(string(out)).Should(ContainSubstring("STARTED"))

		Eventually(func() interface{} {
			return zk.exists(i)
		}, 3, 0.05).Should(BeTrue(), "Expected Zookeeper to be up and running")
	}
	zk.running = true
}

func (zk *ZookeeperClusterRunner) stop(nuke bool) {
	if zk.running {
		for i := 0; i < zk.numNodes; i++ {
			zkExecutable, err := zk.zkExecutable()
			Ω(err).ShouldNot(HaveOccurred())

			cmd := exec.Command(zkExecutable, "stop", zk.configPath(i))
			out, err := cmd.Output()

			Ω(err).ShouldNot(HaveOccurred(), "Zookeeper failed to stop!")
			Ω(string(out)).Should(ContainSubstring("STOPPED"))

			if nuke {
				zk.nukeArtifacts(i)
			}
		}
		zk.running = false
	}
}

func (zk *ZookeeperClusterRunner) writeConfig(index int) {
	config := "tickTime=2000\n"
	config += fmt.Sprintf("dataDir=%s\n", zk.tmpPath(index))
	config += fmt.Sprintf("clientPort=%d\n", zk.clientPort(index))

	if zk.numNodes > 1 {
		config += "initLimit=5\n"
		config += "syncLimit=2\n"
		for node := 1; node <= zk.numNodes; node++ {
			config += fmt.Sprintf("server.%d=127.0.0.1:%d:%d\n", node, zk.serverPort(node), zk.electionPort(node))
		}
	}

	err := ioutil.WriteFile(zk.configPath(index), []byte(config), 0700)
	Ω(err).ShouldNot(HaveOccurred())
}

func (zk *ZookeeperClusterRunner) writeId(index int) {
	err := ioutil.WriteFile(zk.tmpPathTo("myid", index), []byte(fmt.Sprintf("%d", index+1)), 0700)
	Ω(err).ShouldNot(HaveOccurred())
}

func (zk *ZookeeperClusterRunner) clientUrl(index int) string {
	return fmt.Sprintf("127.0.0.1:%d", zk.clientPort(index))
}

func (zk *ZookeeperClusterRunner) clientPort(index int) int {
	return zk.startingPort + index
}

func (zk *ZookeeperClusterRunner) serverPort(index int) int {
	return zk.startingPort + index + 707
}

func (zk *ZookeeperClusterRunner) electionPort(index int) int {
	return zk.startingPort + index + 1707
}

func (zk *ZookeeperClusterRunner) tmpPath(index int) string {
	return fmt.Sprintf("/tmp/ZOOKEEPER_%d", zk.clientPort(index))
}

func (zk *ZookeeperClusterRunner) configPath(index int) string {
	return zk.tmpPath(index) + ".conf"
}

func (zk *ZookeeperClusterRunner) tmpPathTo(subdir string, index int) string {
	return fmt.Sprintf("/%s/%s", zk.tmpPath(index), subdir)
}

func (zk *ZookeeperClusterRunner) nukeArtifacts(index int) {
	os.RemoveAll(zk.tmpPath(index))
	os.Remove(zk.configPath(index))
}

func (zk *ZookeeperClusterRunner) exists(index int) bool {
	_, err := os.Stat(zk.tmpPathTo("zookeeper_server.pid", index))
	return err == nil
}
