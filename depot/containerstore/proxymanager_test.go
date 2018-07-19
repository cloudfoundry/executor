package containerstore_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"code.cloudfoundry.org/executor"
	"code.cloudfoundry.org/executor/depot/containerstore"
	"code.cloudfoundry.org/executor/depot/containerstore/envoy"
	"code.cloudfoundry.org/garden"
	"code.cloudfoundry.org/lager/lagertest"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/tedsuo/ifrit"
	yaml "gopkg.in/yaml.v2"
)

var _ = Describe("ProxyManager", func() {

	var (
		logger                             *lagertest.TestLogger
		containerProcess                   ifrit.Process
		proxyConfigDir                     string
		proxyDir                           string
		configPath                         string
		rotatingCredChan                   chan containerstore.Credential
		container                          executor.Container
		listenerConfigFile                 string
		proxyConfigFile                    string
		proxyManager                       containerstore.ProxyManager
		refreshDelayMS                     time.Duration
		containerProxyTrustedCACerts       []string
		containerProxyVerifySubjectAltName []string
		containerProxyRequireClientCerts   bool
	)

	BeforeEach(func() {
		var err error

		SetDefaultEventuallyTimeout(10 * time.Second)

		container = executor.Container{
			Guid:       fmt.Sprintf("container-guid-%d", GinkgoParallelNode()),
			InternalIP: "10.0.0.1",
			RunInfo: executor.RunInfo{
				EnableContainerProxy: true,
			},
		}

		proxyConfigDir, err = ioutil.TempDir("", "proxymanager-config")
		Expect(err).ToNot(HaveOccurred())

		proxyDir, err = ioutil.TempDir("", "proxymanager-envoy")
		Expect(err).ToNot(HaveOccurred())

		configPath = filepath.Join(proxyConfigDir, container.Guid)

		listenerConfigFile = filepath.Join(configPath, "listeners.yaml")
		proxyConfigFile = filepath.Join(configPath, "envoy.yaml")

		logger = lagertest.NewTestLogger("proxymanager")

		rotatingCredChan = make(chan containerstore.Credential, 1)
		refreshDelayMS = 1000 * time.Millisecond
	})

	JustBeforeEach(func() {
		proxyManager = containerstore.NewProxyManager(
			logger,
			proxyDir,
			proxyConfigDir,
			containerProxyTrustedCACerts,
			containerProxyVerifySubjectAltName,
			containerProxyRequireClientCerts,
			refreshDelayMS,
		)
		Eventually(rotatingCredChan).Should(BeSent(containerstore.Credential{
			Cert: "some-cert",
			Key:  "some-key",
		}))
	})

	AfterEach(func() {
		os.RemoveAll(proxyConfigDir)
		os.RemoveAll(proxyDir)
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

		Context("Runner", func() {
			var (
				runner ifrit.Runner
			)

			JustBeforeEach(func() {
				var err error
				runner, err = proxyManager.Runner(logger, container, rotatingCredChan)
				Expect(err).NotTo(HaveOccurred())

				Expect(proxyConfigFile).NotTo(BeAnExistingFile())
				containerProcess = ifrit.Background(runner)
			})

			It("returns a ProxyRunner that does nothing", func() {
				Eventually(containerProcess.Ready()).Should(BeClosed())
				Consistently(containerProcess.Wait()).ShouldNot(Receive())
				Expect(proxyConfigFile).NotTo(BeAnExistingFile())
			})

			It("continues to read from the rotatingCredChannel", func() {
				Eventually(rotatingCredChan).Should(BeSent(containerstore.Credential{}))
				Eventually(rotatingCredChan).Should(BeSent(containerstore.Credential{}))
			})

			Context("when signaled", func() {
				JustBeforeEach(func() {
					containerProcess.Signal(os.Interrupt)
				})

				It("exits", func() {
					Eventually(containerProcess.Wait()).Should(Receive())
				})
			})
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
			Expect(mounts).To(ConsistOf([]garden.BindMount{
				{
					Origin:  garden.BindMountOriginHost,
					SrcPath: proxyDir,
					DstPath: "/etc/cf-assets/envoy",
				},
				{
					Origin:  garden.BindMountOriginHost,
					SrcPath: filepath.Join(proxyConfigDir, container.Guid),
					DstPath: "/etc/cf-assets/envoy_config",
				},
			}))
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

			var proxyConfig envoy.ProxyConfig

			err = yaml.Unmarshal(data, &proxyConfig)
			Expect(err).NotTo(HaveOccurred())

			admin := proxyConfig.Admin
			Expect(admin.AccessLogPath).To(Equal("/dev/null"))
			Expect(admin.Address).To(Equal(envoy.Address{SocketAddress: envoy.SocketAddress{Address: "127.0.0.1", PortValue: 61003}}))

			Expect(proxyConfig.StaticResources.Clusters).To(HaveLen(1))
			cluster := proxyConfig.StaticResources.Clusters[0]
			Expect(cluster.Name).To(Equal("0-service-cluster"))
			Expect(cluster.ConnectionTimeout).To(Equal("0.25s"))
			Expect(cluster.Type).To(Equal("STATIC"))
			Expect(cluster.LbPolicy).To(Equal("ROUND_ROBIN"))
			Expect(cluster.Hosts).To(Equal([]envoy.Address{
				{SocketAddress: envoy.SocketAddress{Address: "10.0.0.1", PortValue: 8080}},
			}))
			Expect(cluster.CircuitBreakers.Thresholds).To(HaveLen(1))
			Expect(cluster.CircuitBreakers.Thresholds[0].MaxConnections).To(BeNumerically("==", math.MaxUint32))

			Expect(proxyConfig.DynamicResources.LDSConfig).To(Equal(envoy.LDSConfig{
				Path: "/etc/cf-assets/envoy_config/listeners.yaml",
			}))
		})

		Context("for listener config", func() {
			var listenerConfig envoy.ListenerConfig
			BeforeEach(func() {
				containerProxyTrustedCACerts = []string{}
				containerProxyRequireClientCerts = true
				containerProxyVerifySubjectAltName = []string{}
			})

			Context("with invalid trusted cert", func() {
				BeforeEach(func() {
					containerProxyTrustedCACerts = []string{"some-cert"}
				})

				It("should error out", func() {
					runner, proxyRunnerErr := proxyManager.Runner(logger, container, rotatingCredChan)
					Expect(proxyRunnerErr).NotTo(HaveOccurred())
					containerProcess = ifrit.Background(runner)
					Consistently(containerProcess.Ready).ShouldNot(Receive())
					Eventually(logger).Should(gbytes.Say("failed-generating-initial-proxy-listener-config"))
				})
			})

			Context("with valid config", func() {
				JustBeforeEach(func() {
					runner, proxyRunnerErr := proxyManager.Runner(logger, container, rotatingCredChan)
					Expect(proxyRunnerErr).NotTo(HaveOccurred())
					containerProcess = ifrit.Background(runner)
					Eventually(containerProcess.Ready()).Should(BeClosed())
					Eventually(listenerConfigFile).Should(BeAnExistingFile())

					data, err := ioutil.ReadFile(listenerConfigFile)
					Expect(err).NotTo(HaveOccurred())

					err = yaml.Unmarshal(data, &listenerConfig)
					Expect(err).NotTo(HaveOccurred())
					Expect(listenerConfig.Resources).To(HaveLen(1))
				})

				It("writes the initial listener config at start", func() {
					listener := listenerConfig.Resources[0]

					Expect(listener.Type).To(Equal("type.googleapis.com/envoy.api.v2.Listener"))
					Expect(listener.Name).To(Equal("listener-8080"))
					Expect(listener.Address).To(Equal(envoy.Address{SocketAddress: envoy.SocketAddress{Address: "0.0.0.0", PortValue: 61001}}))
					Expect(listener.FilterChains).To(HaveLen(1))

					chain := listener.FilterChains[0]
					certs := chain.TLSContext.CommonTLSContext.TLSCertificates
					validations := chain.TLSContext.CommonTLSContext.ValidationContext
					Expect(chain.TLSContext.RequireClientCertificate).To(BeTrue())
					Expect(certs).To(ConsistOf(envoy.TLSCertificate{
						CertificateChain: envoy.DataSource{InlineString: "some-cert"},
						PrivateKey:       envoy.DataSource{InlineString: "some-key"},
					}))
					Expect(validations.TrustedCA).To(Equal(envoy.DataSource{InlineString: ""}))
					Expect(validations.VerifySubjectAltName).To(BeNil())
					Expect(chain.TLSContext.CommonTLSContext.TLSParams.CipherSuites).To(Equal("[ECDHE-RSA-AES256-GCM-SHA384|ECDHE-RSA-AES128-GCM-SHA256]"))

					Expect(chain.Filters).To(HaveLen(1))
					filter := chain.Filters[0]
					Expect(filter.Name).To(Equal("envoy.tcp_proxy"))
					Expect(filter.Config.Cluster).To(Equal("0-service-cluster"))
					Expect(filter.Config.StatPrefix).NotTo(BeEmpty())
				})

				Context("with container proxy trusted certs set", func() {
					var inlinedCert string
					BeforeEach(func() {
						privateKey, err := rsa.GenerateKey(rand.Reader, 512)
						Expect(err).ToNot(HaveOccurred())
						template := x509.Certificate{
							SerialNumber: new(big.Int),
							Subject:      pkix.Name{CommonName: "sample-cert"},
						}
						derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, privateKey.Public(), privateKey)
						Expect(err).ToNot(HaveOccurred())
						certBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
						cert := string(certBytes)

						inlinedCert = fmt.Sprintf("%s%s", cert, cert)
						containerProxyTrustedCACerts = []string{cert, cert}
						containerProxyVerifySubjectAltName = []string{"valid-alt-name-1", "valid-alt-name-2"}
					})

					It("writes trusted certs info into listener config", func() {
						listener := listenerConfig.Resources[0]
						chain := listener.FilterChains[0]
						validations := chain.TLSContext.CommonTLSContext.ValidationContext
						Expect(validations.TrustedCA).To(Equal(envoy.DataSource{InlineString: inlinedCert}))
						Expect(validations.VerifySubjectAltName).To(ConsistOf("valid-alt-name-1", "valid-alt-name-2"))
					})
				})
			})
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
				Eventually(containerProcess.Ready()).Should(BeClosed())
				Eventually(listenerConfigFile).Should(BeAnExistingFile())

				data, err := ioutil.ReadFile(listenerConfigFile)
				Expect(err).NotTo(HaveOccurred())

				var listenerConfig envoy.ListenerConfig

				err = yaml.Unmarshal(data, &listenerConfig)
				Expect(err).NotTo(HaveOccurred())

				Expect(listenerConfig.Resources).To(HaveLen(2))

				listener := listenerConfig.Resources[0]

				Expect(listener.Type).To(Equal("type.googleapis.com/envoy.api.v2.Listener"))
				Expect(listener.Name).To(Equal("listener-8080"))
				Expect(listener.Address).To(Equal(envoy.Address{SocketAddress: envoy.SocketAddress{Address: "0.0.0.0", PortValue: 61001}}))
				Expect(listener.FilterChains).To(HaveLen(1))
				chain := listener.FilterChains[0]
				certs := chain.TLSContext.CommonTLSContext.TLSCertificates
				Expect(certs).To(ConsistOf(envoy.TLSCertificate{
					CertificateChain: envoy.DataSource{InlineString: "some-cert"},
					PrivateKey:       envoy.DataSource{InlineString: "some-key"},
				}))
				Expect(chain.TLSContext.CommonTLSContext.TLSParams.CipherSuites).To(Equal("[ECDHE-RSA-AES256-GCM-SHA384|ECDHE-RSA-AES128-GCM-SHA256]"))

				Expect(chain.Filters).To(HaveLen(1))
				filter := chain.Filters[0]
				Expect(filter.Name).To(Equal("envoy.tcp_proxy"))
				Expect(filter.Config.Cluster).To(Equal("0-service-cluster"))
				Expect(filter.Config.StatPrefix).NotTo(BeEmpty())

				listener1StatPrefix := filter.Config.StatPrefix

				listener = listenerConfig.Resources[1]

				Expect(listener.Type).To(Equal("type.googleapis.com/envoy.api.v2.Listener"))
				Expect(listener.Name).To(Equal("listener-2222"))
				Expect(listener.Address).To(Equal(envoy.Address{SocketAddress: envoy.SocketAddress{Address: "0.0.0.0", PortValue: 61002}}))
				Expect(listener.FilterChains).To(HaveLen(1))
				chain = listener.FilterChains[0]
				certs = chain.TLSContext.CommonTLSContext.TLSCertificates
				Expect(certs).To(ConsistOf(envoy.TLSCertificate{
					CertificateChain: envoy.DataSource{InlineString: "some-cert"},
					PrivateKey:       envoy.DataSource{InlineString: "some-key"},
				}))
				Expect(chain.TLSContext.CommonTLSContext.TLSParams.CipherSuites).To(Equal("[ECDHE-RSA-AES256-GCM-SHA384|ECDHE-RSA-AES128-GCM-SHA256]"))

				Expect(chain.Filters).To(HaveLen(1))
				filter = chain.Filters[0]
				Expect(filter.Name).To(Equal("envoy.tcp_proxy"))
				Expect(filter.Config.Cluster).To(Equal("1-service-cluster"))
				Expect(filter.Config.StatPrefix).NotTo(BeEmpty())
				Expect(filter.Config.StatPrefix).NotTo(Equal(listener1StatPrefix))
			})

			It("creates the appropriate proxy config file", func() {
				runner, proxyRunnerErr := proxyManager.Runner(logger, container, rotatingCredChan)
				Expect(proxyRunnerErr).NotTo(HaveOccurred())
				containerProcess = ifrit.Background(runner)
				Eventually(containerProcess.Ready()).Should(BeClosed())
				Eventually(proxyConfigFile).Should(BeAnExistingFile())

				data, err := ioutil.ReadFile(proxyConfigFile)
				Expect(err).NotTo(HaveOccurred())

				var proxyConfig envoy.ProxyConfig

				err = yaml.Unmarshal(data, &proxyConfig)
				Expect(err).NotTo(HaveOccurred())

				admin := proxyConfig.Admin
				Expect(admin.AccessLogPath).To(Equal("/dev/null"))
				Expect(admin.Address).To(Equal(envoy.Address{SocketAddress: envoy.SocketAddress{Address: "127.0.0.1", PortValue: 61004}}))

				Expect(proxyConfig.StaticResources.Clusters).To(HaveLen(2))
				cluster := proxyConfig.StaticResources.Clusters[0]
				Expect(cluster.Name).To(Equal("0-service-cluster"))
				Expect(cluster.ConnectionTimeout).To(Equal("0.25s"))
				Expect(cluster.Type).To(Equal("STATIC"))
				Expect(cluster.LbPolicy).To(Equal("ROUND_ROBIN"))
				Expect(cluster.Hosts).To(Equal([]envoy.Address{
					{SocketAddress: envoy.SocketAddress{Address: "10.0.0.1", PortValue: 8080}},
				}))

				cluster = proxyConfig.StaticResources.Clusters[1]
				Expect(cluster.Name).To(Equal("1-service-cluster"))
				Expect(cluster.ConnectionTimeout).To(Equal("0.25s"))
				Expect(cluster.Type).To(Equal("STATIC"))
				Expect(cluster.LbPolicy).To(Equal("ROUND_ROBIN"))
				Expect(cluster.Hosts).To(Equal([]envoy.Address{
					{SocketAddress: envoy.SocketAddress{Address: "10.0.0.1", PortValue: 2222}},
				}))

				Expect(proxyConfig.DynamicResources.LDSConfig).To(Equal(envoy.LDSConfig{
					Path: "/etc/cf-assets/envoy_config/listeners.yaml",
				}))
			})

			Context("when no ports are left", func() {
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
			var initialListenerConfig envoy.ListenerConfig

			JustBeforeEach(func() {
				runner, proxyRunnerErr := proxyManager.Runner(logger, container, rotatingCredChan)
				Expect(proxyRunnerErr).NotTo(HaveOccurred())
				containerProcess = ifrit.Background(runner)

				Eventually(containerProcess.Ready()).Should(BeClosed())

				data, err := ioutil.ReadFile(listenerConfigFile)
				Expect(err).NotTo(HaveOccurred())

				err = yaml.Unmarshal(data, &initialListenerConfig)
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("when creds are not rotated", func() {
			var initialListenerConfig envoy.ListenerConfig
			JustBeforeEach(func() {
				runner, proxyRunnerErr := proxyManager.Runner(logger, container, rotatingCredChan)
				Expect(proxyRunnerErr).NotTo(HaveOccurred())
				containerProcess = ifrit.Background(runner)

				Eventually(containerProcess.Ready()).Should(BeClosed())

				data, err := ioutil.ReadFile(listenerConfigFile)
				Expect(err).NotTo(HaveOccurred())

				err = yaml.Unmarshal(data, &initialListenerConfig)
				Expect(err).NotTo(HaveOccurred())
			})

			It("does not write the listener config to the config path", func() {
				data, err := ioutil.ReadFile(listenerConfigFile)
				Expect(err).NotTo(HaveOccurred())

				var listenerConfig envoy.ListenerConfig

				err = yaml.Unmarshal(data, &listenerConfig)
				Expect(err).NotTo(HaveOccurred())
				Expect(listenerConfig).To(Equal(initialListenerConfig))
			})
		})

		Context("when signaled", func() {
			JustBeforeEach(func() {
				runner, proxyRunnerErr := proxyManager.Runner(logger, container, rotatingCredChan)
				Expect(proxyRunnerErr).NotTo(HaveOccurred())
				containerProcess = ifrit.Background(runner)
				Eventually(containerProcess.Ready()).Should(BeClosed())
				containerProcess.Signal(os.Interrupt)
			})

			It("removes listener config from the filesystem", func() {
				Eventually(listenerConfigFile).ShouldNot(BeAnExistingFile())
				Eventually(proxyConfigFile).ShouldNot(BeAnExistingFile())
				Eventually(filepath.Dir(proxyConfigFile)).ShouldNot(BeADirectory())
			})
		})

	})
})
