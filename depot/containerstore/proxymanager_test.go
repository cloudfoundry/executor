package containerstore_test

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"code.cloudfoundry.org/executor"
	"code.cloudfoundry.org/executor/depot/containerstore"
	"code.cloudfoundry.org/lager"
	"code.cloudfoundry.org/lager/lagertest"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/tedsuo/ifrit"
)

var _ = Describe("ProxyManager", func() {

	var (
		logger             lager.Logger
		tmpdir             string
		rotatingCredChan   chan struct{}
		container          executor.Container
		listenerConfigFile string
		proxyConfigFile    string
		proxyManager       containerstore.ProxyManager
		refreshDelayMS     time.Duration
	)

	BeforeEach(func() {
		var err error

		SetDefaultEventuallyTimeout(10 * time.Second)

		container = executor.Container{
			Guid:       fmt.Sprintf("container-guid-%d", GinkgoParallelNode()),
			InternalIP: "127.0.0.1",
			RunInfo: executor.RunInfo{
				Ports: []executor.PortMapping{{
					ContainerPort:         8080,
					ContainerTLSProxyPort: 61001,
				}},
			},
		}

		tmpdir, err = ioutil.TempDir("", "proxymanager")
		Expect(err).ToNot(HaveOccurred())

		configPath := filepath.Join(tmpdir, container.Guid)

		err = os.MkdirAll(configPath, 0755)
		Expect(err).ToNot(HaveOccurred())

		listenerConfigFile = filepath.Join(configPath, "listeners.json")
		proxyConfigFile = filepath.Join(configPath, "envoy.json")

		logger = lagertest.NewTestLogger("proxymanager")

		rotatingCredChan = make(chan struct{}, 1)
		refreshDelayMS = 1000 * time.Millisecond
	})

	JustBeforeEach(func() {
		proxyManager = containerstore.NewProxyManager(
			logger,
			tmpdir,
			refreshDelayMS,
		)
	})

	AfterEach(func() {
		os.RemoveAll(tmpdir)
	})

	Context("Runner", func() {
		var (
			containerProcess ifrit.Process
		)

		JustBeforeEach(func() {
			runner := proxyManager.Runner(logger, container, rotatingCredChan)
			containerProcess = ifrit.Background(runner)
		})

		AfterEach(func() {
			containerProcess.Signal(os.Interrupt)
			Eventually(containerProcess.Wait()).Should(Receive())
		})

		FIt("creates the appropriate proxy config at start", func() {
			Eventually(containerProcess.Ready()).Should(BeClosed())
			Eventually(proxyConfigFile).Should(BeAnExistingFile())

			data, err := ioutil.ReadFile(proxyConfigFile)
			Expect(err).NotTo(HaveOccurred())

			var proxyConfig containerstore.ProxyConfig

			err = json.Unmarshal(data, &proxyConfig)
			Expect(err).NotTo(HaveOccurred())

			Expect(proxyConfig.Listeners).To(BeEmpty())
			admin := proxyConfig.Admin
			Expect(admin.AccessLogPath).To(Equal("/dev/null"))
			Expect(admin.Address).To(Equal("tcp://127.0.0.1:9901"))

			Expect(proxyConfig.ClusterManager.Clusters).To(HaveLen(2))
			cluster := proxyConfig.ClusterManager.Clusters[0]
			Expect(cluster.Name).To(Equal("0-service-cluster"))
			Expect(cluster.ConnectionTimeoutMs).To(Equal(250))
			Expect(cluster.Type).To(Equal("static"))
			Expect(cluster.LbType).To(Equal("round_robin"))
			Expect(cluster.Hosts).To(Equal([]containerstore.Host{
				{URL: "tcp://127.0.0.1:8080"},
			}))

			cluster = proxyConfig.ClusterManager.Clusters[1]
			Expect(cluster.Name).To(Equal("lds-cluster"))
			Expect(cluster.ConnectionTimeoutMs).To(Equal(250))
			Expect(cluster.Type).To(Equal("static"))
			Expect(cluster.LbType).To(Equal("round_robin"))
			Expect(cluster.Hosts).To(Equal([]containerstore.Host{
				{URL: "tcp://127.0.0.1:61002"},
			}))

			Expect(proxyConfig.LDS).To(Equal(containerstore.LDS{
				Cluster:        "lds-cluster",
				RefreshDelayMS: 1000,
			}))
		})

		It("writes the initial listener config at start", func() {
			Eventually(containerProcess.Ready()).Should(BeClosed())
			Eventually(listenerConfigFile).Should(BeAnExistingFile())

			data, err := ioutil.ReadFile(listenerConfigFile)
			Expect(err).NotTo(HaveOccurred())

			var listenerConfig containerstore.ListenerConfig

			err = json.Unmarshal(data, &listenerConfig)
			Expect(err).NotTo(HaveOccurred())

			Expect(listenerConfig.Listeners).To(HaveLen(1))

			listener := listenerConfig.Listeners[0]

			Expect(listener.Address).To(Equal("tcp://0.0.0.0:61001"))
			Expect(listener.SSLContext.CertChainFile).To(Equal("/etc/cf-instance-credentials/instance.crt"))
			Expect(listener.SSLContext.PrivateKeyFile).To(Equal("/etc/cf-instance-credentials/instance.key"))
			Expect(listener.Filters).To(HaveLen(1))
			filter := listener.Filters[0]
			Expect(filter.Name).To(Equal("tcp_proxy"))
			Expect(filter.Type).To(Equal(containerstore.Read))
			Expect(filter.Config.RouteConfig.Routes).To(HaveLen(1))
			route := filter.Config.RouteConfig.Routes[0]
			Expect(route.Cluster).To(Equal("0-service-cluster"))
		})

		Context("with multiple port mappings", func() {
			BeforeEach(func() {
				container.Ports = []executor.PortMapping{
					executor.PortMapping{
						ContainerPort:         8080,
						ContainerTLSProxyPort: 61001,
					},
					executor.PortMapping{
						ContainerPort:         2222,
						ContainerTLSProxyPort: 61002,
					},
				}
			})

			It("creates the appropriate listener config with a unique stat prefix", func() {
				Eventually(listenerConfigFile).Should(BeAnExistingFile())

				data, err := ioutil.ReadFile(listenerConfigFile)
				Expect(err).NotTo(HaveOccurred())

				var listenerConfig containerstore.ListenerConfig

				err = json.Unmarshal(data, &listenerConfig)
				Expect(err).NotTo(HaveOccurred())
				Expect(listenerConfig.Listeners).To(HaveLen(2))

				listener1 := listenerConfig.Listeners[0]
				listener2 := listenerConfig.Listeners[1]
				Expect(listener1.Address).To(Equal("tcp://0.0.0.0:61001"))
				Expect(listener2.Address).To(Equal("tcp://0.0.0.0:61002"))

				Expect(listener1.SSLContext).To(Equal(listener2.SSLContext))

				Expect(listener1.Filters).To(HaveLen(1))
				filter1 := listener1.Filters[0]

				Expect(listener2.Filters).To(HaveLen(1))
				filter2 := listener2.Filters[0]

				Expect(filter1.Name).To(Equal(filter2.Name))
				Expect(filter1.Type).To(Equal(filter2.Type))

				Expect(filter1.Config.RouteConfig).To(Equal(containerstore.RouteConfig{
					Routes: []containerstore.Route{
						{
							Cluster: "0-service-cluster",
						},
					},
				}))

				Expect(filter2.Config.RouteConfig).To(Equal(containerstore.RouteConfig{
					Routes: []containerstore.Route{
						{
							Cluster: "1-service-cluster",
						},
					},
				}))

				Expect(filter1.Config.StatPrefix).ToNot(BeEmpty())
				Expect(filter1.Config.StatPrefix).ToNot(Equal(filter2.Config.StatPrefix))
			})

			FIt("creates the appropriate proxy file", func() {
				Eventually(containerProcess.Ready()).Should(BeClosed())
				Eventually(proxyConfigFile).Should(BeAnExistingFile())

				data, err := ioutil.ReadFile(proxyConfigFile)
				Expect(err).NotTo(HaveOccurred())

				var proxyConfig containerstore.ProxyConfig

				err = json.Unmarshal(data, &proxyConfig)
				Expect(err).NotTo(HaveOccurred())

				Expect(proxyConfig.Listeners).To(BeEmpty())
				admin := proxyConfig.Admin
				Expect(admin.AccessLogPath).To(Equal("/dev/null"))
				Expect(admin.Address).To(Equal("tcp://127.0.0.1:9901"))

				Expect(proxyConfig.ClusterManager.Clusters).To(HaveLen(3))
				cluster := proxyConfig.ClusterManager.Clusters[0]
				Expect(cluster.Name).To(Equal("0-service-cluster"))
				Expect(cluster.ConnectionTimeoutMs).To(Equal(250))
				Expect(cluster.Type).To(Equal("static"))
				Expect(cluster.LbType).To(Equal("round_robin"))
				Expect(cluster.Hosts).To(Equal([]containerstore.Host{
					{URL: "tcp://127.0.0.1:8080"},
				}))

				cluster = proxyConfig.ClusterManager.Clusters[1]
				Expect(cluster.Name).To(Equal("1-service-cluster"))
				Expect(cluster.ConnectionTimeoutMs).To(Equal(250))
				Expect(cluster.Type).To(Equal("static"))
				Expect(cluster.LbType).To(Equal("round_robin"))
				Expect(cluster.Hosts).To(Equal([]containerstore.Host{
					{URL: "tcp://127.0.0.1:2222"},
				}))

				cluster = proxyConfig.ClusterManager.Clusters[2]
				Expect(cluster.Name).To(Equal("lds-cluster"))
				Expect(cluster.ConnectionTimeoutMs).To(Equal(250))
				Expect(cluster.Type).To(Equal("static"))
				Expect(cluster.LbType).To(Equal("round_robin"))
				Expect(cluster.Hosts).To(Equal([]containerstore.Host{
					{URL: "tcp://127.0.0.1:61003"},
				}))

				Expect(proxyConfig.LDS).To(Equal(containerstore.LDS{
					Cluster:        "lds-cluster",
					RefreshDelayMS: 1000,
				}))
			})

			Context("when there no ports left", func() {
				BeforeEach(func() {
					ports := []executor.PortMapping{}
					for port := uint16(containerstore.StartProxyPort); port < containerstore.EndProxyPort; port += 2 {
						ports = append(ports, executor.PortMapping{
							ContainerPort:         port,
							ContainerTLSProxyPort: port + 1,
						})
					}

					container.Ports = ports
				})

				FIt("returns an error", func() {
					var err error
					Eventually(containerProcess.Wait()).Should(Receive(&err))
					Expect(err).To(Equal(containerstore.ErrNoPortsAvailable))
				})
			})
		})

		Context("when creds are being rotated", func() {
			var initialListenerConfig containerstore.ListenerConfig

			JustBeforeEach(func() {
				Eventually(containerProcess.Ready()).Should(BeClosed())

				data, err := ioutil.ReadFile(listenerConfigFile)
				Expect(err).NotTo(HaveOccurred())

				err = json.Unmarshal(data, &initialListenerConfig)
				Expect(err).NotTo(HaveOccurred())
			})

			It("changes the listener config stat prefix", func() {
				Eventually(rotatingCredChan).Should(BeSent(struct{}{}))

				Eventually(func() []containerstore.Listener {
					data, err := ioutil.ReadFile(listenerConfigFile)
					Expect(err).NotTo(HaveOccurred())
					var listenerConfig containerstore.ListenerConfig
					err = json.Unmarshal(data, &listenerConfig)
					Expect(err).NotTo(HaveOccurred())
					return listenerConfig.Listeners
				}).ShouldNot(ConsistOf(initialListenerConfig.Listeners))
			})
		})

		Context("when creds are not rotated", func() {
			var initialListenerConfig containerstore.ListenerConfig
			JustBeforeEach(func() {
				Eventually(containerProcess.Ready()).Should(BeClosed())

				data, err := ioutil.ReadFile(listenerConfigFile)
				Expect(err).NotTo(HaveOccurred())

				err = json.Unmarshal(data, &initialListenerConfig)
				Expect(err).NotTo(HaveOccurred())
			})

			It("does not write the listener config to the config path", func() {
				data, err := ioutil.ReadFile(listenerConfigFile)
				Expect(err).NotTo(HaveOccurred())

				var listenerConfig containerstore.ListenerConfig

				err = json.Unmarshal(data, &listenerConfig)
				Expect(err).NotTo(HaveOccurred())
				listener := listenerConfig.Listeners[0]
				initialListener := initialListenerConfig.Listeners[0]
				Expect(listener.Address).To(Equal(initialListener.Address))
				Expect(listener.SSLContext).To(Equal(initialListener.SSLContext))

				filter := listener.Filters[0]
				initialFilter := initialListener.Filters[0]
				Expect(filter.Type).To(Equal(initialFilter.Type))
				Expect(filter.Name).To(Equal(initialFilter.Name))
				Expect(filter.Config.RouteConfig).To(Equal(initialFilter.Config.RouteConfig))

				Expect(filter.Config.StatPrefix).To(Equal(initialFilter.Config.StatPrefix))
			})
		})

		Context("when signaled", func() {
			JustBeforeEach(func() {
				rotatingCredChan <- struct{}{}
				Eventually(containerProcess.Ready()).Should(BeClosed())
				containerProcess.Signal(os.Interrupt)
			})

			It("removes listener config from the filesystem", func() {
				Eventually(listenerConfigFile).ShouldNot(BeAnExistingFile())
				Eventually(proxyConfigFile).ShouldNot(BeAnExistingFile())
			})
		})

	})
})
