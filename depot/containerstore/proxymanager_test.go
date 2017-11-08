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
	"code.cloudfoundry.org/garden"
	"code.cloudfoundry.org/lager"
	"code.cloudfoundry.org/lager/lagertest"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/tedsuo/ifrit"
)

var _ = Describe("ProxyManager", func() {

	var (
		logger             lager.Logger
		containerProcess   ifrit.Process
		proxyConfigDir     string
		proxyDir           string
		configPath         string
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
				EnableContainerProxy: true,
			},
		}

		proxyConfigDir, err = ioutil.TempDir("", "proxymanager-config")
		Expect(err).ToNot(HaveOccurred())

		proxyDir, err = ioutil.TempDir("", "proxymanager-envoy")
		Expect(err).ToNot(HaveOccurred())

		configPath = filepath.Join(proxyConfigDir, container.Guid)

		listenerConfigFile = filepath.Join(configPath, "listeners.json")
		proxyConfigFile = filepath.Join(configPath, "envoy.json")

		logger = lagertest.NewTestLogger("proxymanager")

		rotatingCredChan = make(chan struct{}, 1)
		refreshDelayMS = 1000 * time.Millisecond
	})

	JustBeforeEach(func() {
		proxyManager = containerstore.NewProxyManager(
			logger,
			proxyDir,
			proxyConfigDir,
			refreshDelayMS,
		)
	})

	AfterEach(func() {
		os.RemoveAll(proxyConfigDir)
	})

	Context("No-op Proxy Manager", func() {
		JustBeforeEach(func() {
			proxyManager = containerstore.NewNoopProxyManager()
		})

		AfterEach(func() {
			if containerProcess != nil {
				containerProcess.Signal(os.Interrupt)
				Eventually(containerProcess.Wait()).Should(Receive())
			}
		})

		It("returns a ProxyRunner that does nothing", func() {
			runner, proxyRunnerErr := proxyManager.Runner(logger, container, rotatingCredChan)
			Expect(proxyRunnerErr).NotTo(HaveOccurred())

			Expect(proxyConfigFile).NotTo(BeAnExistingFile())
			containerProcess = ifrit.Background(runner)
			Eventually(containerProcess.Ready()).Should(BeClosed())
			Consistently(containerProcess.Wait()).ShouldNot(Receive())
			Expect(proxyConfigFile).NotTo(BeAnExistingFile())
		})

		It("returns an empty bind mount", func() {
			mounts, err := proxyManager.BindMounts(logger, container)
			Expect(err).NotTo(HaveOccurred())
			Expect(mounts).To(BeEmpty())
		})

		It("returns an empty proxy port mapping", func() {
			ports, extraPorts := proxyManager.ProxyPorts(logger, &container)
			Expect(ports).To(BeEmpty())
			Expect(extraPorts).To(BeEmpty())
		})
	})

	Context("BindMounts", func() {
		Context("the EnableContainerProxy is disabled on the container", func() {
			BeforeEach(func() {
				container.EnableContainerProxy = false
			})

			It("returns an empty bind mount", func() {
				mounts, err := proxyManager.BindMounts(logger, container)
				Expect(err).NotTo(HaveOccurred())
				Expect(mounts).To(BeEmpty())
			})
		})

		It("returns the appropriate bind mounts for container proxy", func() {
			mounts, err := proxyManager.BindMounts(logger, container)
			Expect(err).NotTo(HaveOccurred())
			Expect(mounts).To(ConsistOf(
				garden.BindMount{
					Origin:  garden.BindMountOriginHost,
					SrcPath: proxyDir,
					DstPath: "/etc/cf-assets/envoy",
				},
				garden.BindMount{
					Origin:  garden.BindMountOriginHost,
					SrcPath: fmt.Sprintf("%s/%s", proxyConfigDir, container.Guid),
					DstPath: "/etc/cf-assets/envoy_config",
				},
			))
		})

		It("makes the proxy config directory on the host", func() {
			_, err := proxyManager.BindMounts(logger, container)
			Expect(err).NotTo(HaveOccurred())
			proxyConfigDir := fmt.Sprintf("%s/%s", proxyConfigDir, container.Guid)
			Expect(proxyConfigDir).To(BeADirectory())
		})

		Context("when the manager fails to create the proxy config directory", func() {
			BeforeEach(func() {
				_, err := os.Create(configPath)
				Expect(err).NotTo(HaveOccurred())
			})

			It("returns an error", func() {
				_, err := proxyManager.BindMounts(logger, container)
				Expect(err).To(HaveOccurred())
			})
		})
	})

	Context("ProxyPorts", func() {
		BeforeEach(func() {
			container.Ports = []executor.PortMapping{
				{
					ContainerPort: 8080,
				},
				{
					ContainerPort: 9090,
				},
			}
		})

		Context("the EnableContainerProxy is disabled on the container", func() {
			BeforeEach(func() {
				container.EnableContainerProxy = false
			})

			It("returns an empty proxy port mapping", func() {
				ports, extraPorts := proxyManager.ProxyPorts(logger, &container)
				Expect(ports).To(BeEmpty())
				Expect(extraPorts).To(BeEmpty())
			})
		})

		It("each port gets an equivalent extra proxy port", func() {
			ports, extraPorts := proxyManager.ProxyPorts(logger, &container)
			Expect(ports).To(ConsistOf([]executor.ProxyPortMapping{
				{
					AppPort:   8080,
					ProxyPort: 61001,
				},
				{
					AppPort:   9090,
					ProxyPort: 61002,
				},
			}))

			Expect(extraPorts).To(ConsistOf([]uint16{61001, 61002}))
		})

		Context("when the requested ports are in the 6100n range", func() {
			BeforeEach(func() {

				container.Ports = []executor.PortMapping{
					{ContainerPort: 61001},
					{ContainerPort: 9090},
				}
			})

			It("the additional proxy ports don't collide with requested ports", func() {
				ports, extraPorts := proxyManager.ProxyPorts(logger, &container)
				Expect(ports).To(ConsistOf([]executor.ProxyPortMapping{
					{
						AppPort:   61001,
						ProxyPort: 61002,
					},
					{
						AppPort:   9090,
						ProxyPort: 61003,
					},
				}))

				Expect(extraPorts).To(ConsistOf([]uint16{61002, 61003}))
			})
		})
	})

	Context("Runner", func() {
		BeforeEach(func() {
			err := os.MkdirAll(configPath, 0755)
			Expect(err).ToNot(HaveOccurred())

			container.Ports = []executor.PortMapping{
				{
					ContainerPort:         8080,
					ContainerTLSProxyPort: 61001,
				},
			}
		})

		AfterEach(func() {
			if containerProcess != nil {
				containerProcess.Signal(os.Interrupt)
				Eventually(containerProcess.Wait()).Should(Receive())
			}
		})

		Context("the EnableContainerProxy is disabled on the container", func() {
			BeforeEach(func() {
				container.EnableContainerProxy = false
			})

			It("returns a ProxyRunner that does nothing", func() {
				runner, proxyRunnerErr := proxyManager.Runner(logger, container, rotatingCredChan)
				Expect(proxyRunnerErr).NotTo(HaveOccurred())

				Expect(proxyConfigFile).NotTo(BeAnExistingFile())
				containerProcess = ifrit.Background(runner)
				Eventually(containerProcess.Ready()).Should(BeClosed())
				Consistently(containerProcess.Wait()).ShouldNot(Receive())
				Expect(proxyConfigFile).NotTo(BeAnExistingFile())
			})
		})

		It("creates the appropriate proxy config at start", func() {
			runner, proxyRunnerErr := proxyManager.Runner(logger, container, rotatingCredChan)
			Expect(proxyRunnerErr).NotTo(HaveOccurred())
			containerProcess = ifrit.Background(runner)
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
			runner, proxyRunnerErr := proxyManager.Runner(logger, container, rotatingCredChan)
			Expect(proxyRunnerErr).NotTo(HaveOccurred())
			containerProcess = ifrit.Background(runner)
			Eventually(containerProcess.Ready()).Should(BeClosed())
			Eventually(listenerConfigFile).Should(BeAnExistingFile())

			data, err := ioutil.ReadFile(listenerConfigFile)
			Expect(err).NotTo(HaveOccurred())

			var listenerConfig containerstore.ListenerConfig

			err = json.Unmarshal(data, &listenerConfig)
			Expect(err).NotTo(HaveOccurred())

			Expect(listenerConfig.Listeners).To(HaveLen(1))

			listener := listenerConfig.Listeners[0]

			Expect(listener.Name).To(Equal("listener-8080"))
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

		It("exposes proxyConfigPort", func() {
			runner, proxyRunnerErr := proxyManager.Runner(logger, container, rotatingCredChan)
			Expect(proxyRunnerErr).NotTo(HaveOccurred())
			containerProcess = ifrit.Background(runner)
			Expect(runner.Port()).To(Equal(uint16(61002)))
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
				runner, proxyRunnerErr := proxyManager.Runner(logger, container, rotatingCredChan)
				Expect(proxyRunnerErr).NotTo(HaveOccurred())
				containerProcess = ifrit.Background(runner)
				Eventually(listenerConfigFile).Should(BeAnExistingFile())

				data, err := ioutil.ReadFile(listenerConfigFile)
				Expect(err).NotTo(HaveOccurred())

				var listenerConfig containerstore.ListenerConfig

				err = json.Unmarshal(data, &listenerConfig)
				Expect(err).NotTo(HaveOccurred())
				Expect(listenerConfig.Listeners).To(HaveLen(2))

				listener1 := listenerConfig.Listeners[0]
				listener2 := listenerConfig.Listeners[1]
				Expect(listener1.Name).To(Equal("listener-8080"))
				Expect(listener2.Name).To(Equal("listener-2222"))
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

			It("creates the appropriate proxy file", func() {
				runner, proxyRunnerErr := proxyManager.Runner(logger, container, rotatingCredChan)
				Expect(proxyRunnerErr).NotTo(HaveOccurred())
				containerProcess = ifrit.Background(runner)
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

				It("returns an error", func() {
					_, proxyRunnerErr := proxyManager.Runner(logger, container, rotatingCredChan)
					Expect(proxyRunnerErr).To(Equal(containerstore.ErrNoPortsAvailable))
				})
			})
		})

		Context("when creds are being rotated", func() {
			var initialListenerConfig containerstore.ListenerConfig

			JustBeforeEach(func() {
				runner, proxyRunnerErr := proxyManager.Runner(logger, container, rotatingCredChan)
				Expect(proxyRunnerErr).NotTo(HaveOccurred())
				containerProcess = ifrit.Background(runner)

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
				runner, proxyRunnerErr := proxyManager.Runner(logger, container, rotatingCredChan)
				Expect(proxyRunnerErr).NotTo(HaveOccurred())
				containerProcess = ifrit.Background(runner)

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
				runner, proxyRunnerErr := proxyManager.Runner(logger, container, rotatingCredChan)
				Expect(proxyRunnerErr).NotTo(HaveOccurred())
				containerProcess = ifrit.Background(runner)
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
