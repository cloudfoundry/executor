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

	"code.cloudfoundry.org/clock/fakeclock"
	"code.cloudfoundry.org/executor"
	"code.cloudfoundry.org/executor/depot/containerstore"
	"code.cloudfoundry.org/executor/depot/containerstore/envoy"
	"code.cloudfoundry.org/garden"
	"code.cloudfoundry.org/lager/lagertest"
	uuid "github.com/nu7hatch/gouuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	yaml "gopkg.in/yaml.v2"
)

var _ = Describe("ProxyConfigHandler", func() {

	var (
		logger                             *lagertest.TestLogger
		proxyConfigDir                     string
		proxyDir                           string
		configPath                         string
		rotatingCredChan                   chan containerstore.Credential
		container                          executor.Container
		sdsServerCertAndKeyFile            string
		sdsServerValidationContextFile     string
		proxyConfigFile                    string
		proxyConfigHandler                 *containerstore.ProxyConfigHandler
		reloadDuration                     time.Duration
		reloadClock                        *fakeclock.FakeClock
		containerProxyTrustedCACerts       []string
		containerProxyVerifySubjectAltName []string
		containerProxyRequireClientCerts   bool
		adsServers                         []string
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

		proxyConfigFile = filepath.Join(configPath, "envoy.yaml")
		sdsServerCertAndKeyFile = filepath.Join(configPath, "sds-server-cert-and-key.yaml")
		sdsServerValidationContextFile = filepath.Join(configPath, "sds-server-validation-context.yaml")

		logger = lagertest.NewTestLogger("proxymanager")

		rotatingCredChan = make(chan containerstore.Credential, 1)
		reloadDuration = 1 * time.Second
		reloadClock = fakeclock.NewFakeClock(time.Now())

		containerProxyTrustedCACerts = []string{}
		containerProxyVerifySubjectAltName = []string{}
		containerProxyRequireClientCerts = false

		adsServers = []string{
			"10.255.217.2:15010",
			"10.255.217.3:15010",
		}
	})

	JustBeforeEach(func() {
		proxyConfigHandler = containerstore.NewProxyConfigHandler(
			logger,
			proxyDir,
			proxyConfigDir,
			containerProxyTrustedCACerts,
			containerProxyVerifySubjectAltName,
			containerProxyRequireClientCerts,
			reloadDuration,
			reloadClock,
			adsServers,
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

	Describe("NoopProxyConfigHandler", func() {
		var (
			proxyConfigHandler *containerstore.NoopProxyConfigHandler
		)

		JustBeforeEach(func() {
			proxyConfigHandler = containerstore.NewNoopProxyConfigHandler()
		})

		Describe("CreateDir", func() {
			It("returns an empty bind mount", func() {
				mounts, _, err := proxyConfigHandler.CreateDir(logger, container)
				Expect(err).NotTo(HaveOccurred())
				Expect(mounts).To(BeEmpty())
			})

			It("returns an empty environment variables", func() {
				_, env, err := proxyConfigHandler.CreateDir(logger, container)
				Expect(err).NotTo(HaveOccurred())
				Expect(env).To(BeEmpty())
			})
		})

		Describe("Close", func() {
			JustBeforeEach(func() {
				Expect(proxyConfigFile).NotTo(BeAnExistingFile())
				err := proxyConfigHandler.Update(containerstore.Credential{Cert: "cert", Key: "key"}, container)
				Expect(err).NotTo(HaveOccurred())
			})

			It("does nothing", func() {
				Expect(proxyConfigFile).NotTo(BeAnExistingFile())
			})
		})

		Describe("Update", func() {
			JustBeforeEach(func() {
				Expect(proxyConfigFile).NotTo(BeAnExistingFile())
				err := proxyConfigHandler.Update(containerstore.Credential{Cert: "cert", Key: "key"}, container)
				Expect(err).NotTo(HaveOccurred())
			})

			It("does nothing", func() {
				Expect(proxyConfigFile).NotTo(BeAnExistingFile())
			})
		})

		It("returns an empty proxy port mapping", func() {
			ports, extraPorts := proxyConfigHandler.ProxyPorts(logger, &container)
			Expect(ports).To(BeEmpty())
			Expect(extraPorts).To(BeEmpty())
		})
	})

	Describe("CreateDir", func() {
		Context("the EnableContainerProxy is disabled on the container", func() {
			BeforeEach(func() {
				container.EnableContainerProxy = false
			})

			It("returns an empty bind mount", func() {
				mounts, _, err := proxyConfigHandler.CreateDir(logger, container)
				Expect(err).NotTo(HaveOccurred())
				Expect(mounts).To(BeEmpty())
			})
		})

		It("returns the appropriate bind mounts for container proxy", func() {
			mounts, _, err := proxyConfigHandler.CreateDir(logger, container)
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
			_, _, err := proxyConfigHandler.CreateDir(logger, container)
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
				_, _, err := proxyConfigHandler.CreateDir(logger, container)
				Expect(err).To(HaveOccurred())
			})
		})
	})

	Describe("RemoveDir", func() {
		It("removes the directory created by CreateDir", func() {
			_, _, err := proxyConfigHandler.CreateDir(logger, container)
			Expect(err).NotTo(HaveOccurred())
			Expect(configPath).To(BeADirectory())

			err = proxyConfigHandler.RemoveDir(logger, container)
			Expect(err).NotTo(HaveOccurred())
			Expect(configPath).NotTo(BeADirectory())
		})

		Context("the EnableContainerProxy is disabled on the container", func() {
			BeforeEach(func() {
				container.EnableContainerProxy = false
			})

			It("does not return an error when deleting a non existing directory", func() {
				err := proxyConfigHandler.RemoveDir(logger, container)
				Expect(err).NotTo(HaveOccurred())
			})
		})
	})

	Describe("ProxyPorts", func() {
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
				ports, extraPorts := proxyConfigHandler.ProxyPorts(logger, &container)
				Expect(ports).To(BeEmpty())
				Expect(extraPorts).To(BeEmpty())
			})
		})

		It("each port gets an equivalent extra proxy port", func() {
			ports, extraPorts := proxyConfigHandler.ProxyPorts(logger, &container)
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
				ports, extraPorts := proxyConfigHandler.ProxyPorts(logger, &container)
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

	Describe("Update", func() {
		BeforeEach(func() {
			err := os.MkdirAll(configPath, 0755)
			Expect(err).ToNot(HaveOccurred())

			container.Ports = []executor.PortMapping{
				{
					ContainerPort:         8080,
					ContainerTLSProxyPort: 61001,
				},
			}

			containerProxyRequireClientCerts = true
		})

		Context("the EnableContainerProxy is disabled on the container", func() {
			BeforeEach(func() {
				container.EnableContainerProxy = false
			})

			It("does not write a envoy config file", func() {
				err := proxyConfigHandler.Update(containerstore.Credential{Cert: "", Key: ""}, container)
				Expect(err).NotTo(HaveOccurred())

				Expect(proxyConfigFile).NotTo(BeAnExistingFile())
			})
		})

		Context("with containerProxyRequireClientCerts set to false", func() {
			BeforeEach(func() {
				containerProxyRequireClientCerts = false
			})

			It("creates a proxy config without tls_context", func() {
				err := proxyConfigHandler.Update(containerstore.Credential{Cert: "cert", Key: "key"}, container)
				Expect(err).NotTo(HaveOccurred())

				Eventually(proxyConfigFile).Should(BeAnExistingFile())

				var proxyConfig envoy.ProxyConfig
				Expect(yamlFileToStruct(proxyConfigFile, &proxyConfig)).To(Succeed())

				admin := proxyConfig.Admin
				Expect(admin.AccessLogPath).To(Equal(os.DevNull))
				Expect(admin.Address).To(Equal(envoy.Address{SocketAddress: envoy.SocketAddress{Address: "127.0.0.1", PortValue: 61002}}))

				Expect(proxyConfig.StaticResources.Clusters).To(HaveLen(2))
				cluster := proxyConfig.StaticResources.Clusters[0]
				Expect(cluster.Name).To(Equal("0-service-cluster"))
				Expect(time.ParseDuration(cluster.ConnectionTimeout)).To(Equal(250 * time.Millisecond))
				Expect(cluster.Type).To(BeEmpty())
				Expect(cluster.LbPolicy).To(BeEmpty())
				Expect(cluster.Hosts).To(Equal([]envoy.Address{
					{SocketAddress: envoy.SocketAddress{Address: "10.0.0.1", PortValue: 8080}},
				}))
				Expect(cluster.CircuitBreakers.Thresholds).To(HaveLen(1))
				Expect(cluster.CircuitBreakers.Thresholds[0].MaxConnections).To(BeNumerically("==", math.MaxUint32))

				adsCluster := proxyConfig.StaticResources.Clusters[1]
				Expect(adsCluster.Name).To(Equal("pilot-ads"))
				Expect(time.ParseDuration(adsCluster.ConnectionTimeout)).To(Equal(250 * time.Millisecond))
				Expect(adsCluster.Type).To(BeEmpty())
				Expect(adsCluster.LbPolicy).To(BeEmpty())
				Expect(adsCluster.Hosts).To(Equal([]envoy.Address{
					{SocketAddress: envoy.SocketAddress{Address: "10.255.217.2", PortValue: 15010}},
					{SocketAddress: envoy.SocketAddress{Address: "10.255.217.3", PortValue: 15010}},
				}))
				Expect(adsCluster.HTTP2ProtocolOptions).To(Equal(envoy.HTTP2ProtocolOptions{}))

				Expect(proxyConfig.StaticResources.Listeners).To(HaveLen(1))
				listener := proxyConfig.StaticResources.Listeners[0]
				Expect(listener.Name).To(Equal("listener-8080"))
				Expect(listener.Address.SocketAddress.Address).To(Equal("0.0.0.0"))
				Expect(listener.Address.SocketAddress.PortValue).To(Equal(uint16(61001)))
				Expect(listener.FilterChains).To(Equal([]envoy.FilterChain{
					{
						Filters: []envoy.Filter{
							{
								Name:   "envoy.tcp_proxy",
								Config: envoy.Config{StatPrefix: "0-stats", Cluster: "0-service-cluster"},
							},
						},
						TLSContext: envoy.TLSContext{
							CommonTLSContext: envoy.CommonTLSContext{
								TLSCertificateSDSSecretConfigs: []envoy.SecretConfig{
									{
										Name:      "server-cert-and-key",
										SDSConfig: envoy.SDSConfig{Path: "/etc/cf-assets/envoy_config/sds-server-cert-and-key.yaml"},
									},
								},
								TLSParams: envoy.TLSParams{
									CipherSuites: []string{"ECDHE-RSA-AES256-GCM-SHA384", "ECDHE-RSA-AES128-GCM-SHA256"},
								},
							},
							RequireClientCertificate: false,
						},
					},
				}))
			})

		})

		It("creates appropriate sds-server-cert-and-key.yaml configuration file", func() {
			err := proxyConfigHandler.Update(containerstore.Credential{Cert: "cert", Key: "key"}, container)
			Expect(err).NotTo(HaveOccurred())
			Eventually(sdsServerCertAndKeyFile).Should(BeAnExistingFile())

			var sdsCertificateResource envoy.SDSCertificateResource
			Expect(yamlFileToStruct(sdsServerCertAndKeyFile, &sdsCertificateResource)).To(Succeed())

			Expect(sdsCertificateResource.VersionInfo).To(Equal("0"))

			resource := sdsCertificateResource.Resources[0]
			Expect(resource.Type).To(Equal("type.googleapis.com/envoy.api.v2.auth.Secret"))
			Expect(resource.Name).To(Equal("server-cert-and-key"))
			certs := resource.TLSCertificate
			Expect(certs).To(Equal(envoy.TLSCertificate{
				CertificateChain: envoy.DataSource{InlineString: "cert"},
				PrivateKey:       envoy.DataSource{InlineString: "key"},
			}))
		})

		Context("with container proxy trusted certs set", func() {
			var inlinedCert string

			BeforeEach(func() {
				cert, _, _ := generateCertAndKey()
				chainedCert := fmt.Sprintf("%s%s", cert, cert)
				containerProxyTrustedCACerts = []string{cert, chainedCert}
				inlinedCert = fmt.Sprintf("%s%s", cert, chainedCert)
				containerProxyVerifySubjectAltName = []string{"valid-alt-name-1", "valid-alt-name-2"}
			})

			It("creates appropriate sds-server-validation-context.yaml configuration file", func() {
				err := proxyConfigHandler.Update(containerstore.Credential{Cert: "cert", Key: "key"}, container)
				Expect(err).NotTo(HaveOccurred())
				Eventually(sdsServerValidationContextFile).Should(BeAnExistingFile())

				var sdsCAResource envoy.SDSCAResource
				Expect(yamlFileToStruct(sdsServerValidationContextFile, &sdsCAResource)).To(Succeed())

				Expect(sdsCAResource.VersionInfo).To(Equal("0"))

				resource := sdsCAResource.Resources[0]
				Expect(resource.Type).To(Equal("type.googleapis.com/envoy.api.v2.auth.Secret"))
				Expect(resource.Name).To(Equal("server-validation-context"))
				validations := resource.ValidationContext
				Expect(validations.TrustedCA).To(Equal(envoy.DataSource{InlineString: inlinedCert}))
				Expect(validations.VerifySubjectAltName).To(ConsistOf("valid-alt-name-1", "valid-alt-name-2"))
			})
		})

		It("creates the appropriate proxy config at start", func() {
			err := proxyConfigHandler.Update(containerstore.Credential{Cert: "cert", Key: "key"}, container)
			Expect(err).NotTo(HaveOccurred())

			Eventually(proxyConfigFile).Should(BeAnExistingFile())

			var proxyConfig envoy.ProxyConfig
			Expect(yamlFileToStruct(proxyConfigFile, &proxyConfig)).To(Succeed())

			admin := proxyConfig.Admin
			Expect(admin.AccessLogPath).To(Equal(os.DevNull))
			Expect(admin.Address).To(Equal(envoy.Address{SocketAddress: envoy.SocketAddress{Address: "127.0.0.1", PortValue: 61002}}))

			Expect(proxyConfig.StaticResources.Clusters).To(HaveLen(2))
			cluster := proxyConfig.StaticResources.Clusters[0]
			Expect(cluster.Name).To(Equal("0-service-cluster"))
			Expect(time.ParseDuration(cluster.ConnectionTimeout)).To(Equal(250 * time.Millisecond))
			Expect(cluster.Type).To(BeEmpty())
			Expect(cluster.LbPolicy).To(BeEmpty())
			Expect(cluster.Hosts).To(Equal([]envoy.Address{
				{SocketAddress: envoy.SocketAddress{Address: "10.0.0.1", PortValue: 8080}},
			}))
			Expect(cluster.CircuitBreakers.Thresholds).To(HaveLen(1))
			Expect(cluster.CircuitBreakers.Thresholds[0].MaxConnections).To(BeNumerically("==", math.MaxUint32))

			adsCluster := proxyConfig.StaticResources.Clusters[1]
			Expect(adsCluster.Name).To(Equal("pilot-ads"))
			Expect(time.ParseDuration(adsCluster.ConnectionTimeout)).To(Equal(250 * time.Millisecond))
			Expect(adsCluster.Type).To(BeEmpty())
			Expect(adsCluster.LbPolicy).To(BeEmpty())
			Expect(adsCluster.Hosts).To(Equal([]envoy.Address{
				{SocketAddress: envoy.SocketAddress{Address: "10.255.217.2", PortValue: 15010}},
				{SocketAddress: envoy.SocketAddress{Address: "10.255.217.3", PortValue: 15010}},
			}))
			Expect(adsCluster.HTTP2ProtocolOptions).To(Equal(envoy.HTTP2ProtocolOptions{}))

			Expect(proxyConfig.StaticResources.Listeners).To(HaveLen(1))
			listener := proxyConfig.StaticResources.Listeners[0]
			Expect(listener.Name).To(Equal("listener-8080"))
			Expect(listener.Address.SocketAddress.Address).To(Equal("0.0.0.0"))
			Expect(listener.Address.SocketAddress.PortValue).To(Equal(uint16(61001)))
			Expect(listener.FilterChains).To(Equal([]envoy.FilterChain{
				{
					Filters: []envoy.Filter{
						{
							Name:   "envoy.tcp_proxy",
							Config: envoy.Config{StatPrefix: "0-stats", Cluster: "0-service-cluster"},
						},
					},
					TLSContext: envoy.TLSContext{
						CommonTLSContext: envoy.CommonTLSContext{
							TLSCertificateSDSSecretConfigs: []envoy.SecretConfig{
								{
									Name:      "server-cert-and-key",
									SDSConfig: envoy.SDSConfig{Path: "/etc/cf-assets/envoy_config/sds-server-cert-and-key.yaml"},
								},
							},
							TLSParams: envoy.TLSParams{
								CipherSuites: []string{"ECDHE-RSA-AES256-GCM-SHA384", "ECDHE-RSA-AES128-GCM-SHA256"},
							},
							ValidationContextSDSSecretConfig: envoy.SecretConfig{
								Name:      "server-validation-context",
								SDSConfig: envoy.SDSConfig{Path: "/etc/cf-assets/envoy_config/sds-server-validation-context.yaml"},
							},
						},
						RequireClientCertificate: true,
					},
				},
			}))

			Expect(proxyConfig.DynamicResources.LDSConfig).To(Equal(envoy.LDSConfig{envoy.ADS{}}))
			Expect(proxyConfig.DynamicResources.CDSConfig).To(Equal(envoy.CDSConfig{envoy.ADS{}}))
			Expect(proxyConfig.DynamicResources.ADSConfig).To(Equal(envoy.ADSConfig{
				APIType: "GRPC", GRPCServices: []envoy.GRPCService{
					{
						EnvoyGRPC: envoy.EnvoyGRPC{ClusterName: "pilot-ads"},
					},
				},
			}))
		})

		Context("when ads server addresses is empty", func() {
			BeforeEach(func() {
				adsServers = []string{}
			})

			It("creates a proxy config without the ads config", func() {
				err := proxyConfigHandler.Update(containerstore.Credential{Cert: "cert", Key: "key"}, container)
				Expect(err).NotTo(HaveOccurred())
				Eventually(proxyConfigFile).Should(BeAnExistingFile())

				var proxyConfig envoy.ProxyConfig
				Expect(yamlFileToStruct(proxyConfigFile, &proxyConfig)).To(Succeed())

				Expect(proxyConfig.StaticResources.Clusters).To(HaveLen(1))
				cluster := proxyConfig.StaticResources.Clusters[0]
				Expect(cluster.Name).To(Equal("0-service-cluster"))

				var nilPointerDynamicResources *envoy.DynamicResources
				Expect(proxyConfig.DynamicResources).To(Equal(nilPointerDynamicResources))
			})
		})

		Context("when an ads server address is malformed", func() {
			BeforeEach(func() {
				adsServers = []string{
					"malformed",
				}
			})

			It("returns an error", func() {
				err := proxyConfigHandler.Update(containerstore.Credential{Cert: "cert", Key: "key"}, container)
				Expect(err).To(MatchError("ads server address is invalid: malformed"))
			})
		})

		Context("when an ads server port is malformed", func() {
			BeforeEach(func() {
				adsServers = []string{
					"0.0.0.0:mal",
				}
			})

			It("returns an error", func() {
				err := proxyConfigHandler.Update(containerstore.Credential{Cert: "cert", Key: "key"}, container)
				Expect(err).To(MatchError("ads server address is invalid: 0.0.0.0:mal"))
			})
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

			It("creates the appropriate proxy config file", func() {
				err := proxyConfigHandler.Update(containerstore.Credential{Cert: "cert", Key: "key"}, container)
				Expect(err).NotTo(HaveOccurred())
				Eventually(proxyConfigFile).Should(BeAnExistingFile())

				var proxyConfig envoy.ProxyConfig
				Expect(yamlFileToStruct(proxyConfigFile, &proxyConfig)).To(Succeed())

				admin := proxyConfig.Admin
				Expect(admin.AccessLogPath).To(Equal(os.DevNull))
				Expect(admin.Address).To(Equal(envoy.Address{SocketAddress: envoy.SocketAddress{Address: "127.0.0.1", PortValue: 61003}}))

				Expect(proxyConfig.StaticResources.Clusters).To(HaveLen(3))

				cluster := proxyConfig.StaticResources.Clusters[0]
				Expect(cluster.Name).To(Equal("0-service-cluster"))
				Expect(time.ParseDuration(cluster.ConnectionTimeout)).To(Equal(250 * time.Millisecond))
				Expect(cluster.Type).To(BeEmpty())
				Expect(cluster.LbPolicy).To(BeEmpty())
				Expect(cluster.Hosts).To(Equal([]envoy.Address{
					{SocketAddress: envoy.SocketAddress{Address: "10.0.0.1", PortValue: 8080}},
				}))

				cluster = proxyConfig.StaticResources.Clusters[1]
				Expect(cluster.Name).To(Equal("1-service-cluster"))
				Expect(time.ParseDuration(cluster.ConnectionTimeout)).To(Equal(250 * time.Millisecond))
				Expect(cluster.Type).To(BeEmpty())
				Expect(cluster.LbPolicy).To(BeEmpty())
				Expect(cluster.Hosts).To(Equal([]envoy.Address{
					{SocketAddress: envoy.SocketAddress{Address: "10.0.0.1", PortValue: 2222}},
				}))

				adsCluster := proxyConfig.StaticResources.Clusters[2]
				Expect(adsCluster.Name).To(Equal("pilot-ads"))
				Expect(time.ParseDuration(adsCluster.ConnectionTimeout)).To(Equal(250 * time.Millisecond))
				Expect(adsCluster.Type).To(BeEmpty())
				Expect(adsCluster.LbPolicy).To(BeEmpty())
				Expect(adsCluster.Hosts).To(Equal([]envoy.Address{
					{SocketAddress: envoy.SocketAddress{Address: "10.255.217.2", PortValue: 15010}},
					{SocketAddress: envoy.SocketAddress{Address: "10.255.217.3", PortValue: 15010}},
				}))
				Expect(adsCluster.HTTP2ProtocolOptions).To(Equal(envoy.HTTP2ProtocolOptions{}))

				Expect(proxyConfig.StaticResources.Listeners).To(HaveLen(2))
				listener := proxyConfig.StaticResources.Listeners[0]
				Expect(listener.Name).To(Equal("listener-8080"))
				Expect(listener.Address.SocketAddress.Address).To(Equal("0.0.0.0"))
				Expect(listener.Address.SocketAddress.PortValue).To(Equal(uint16(61001)))
				Expect(listener.FilterChains).To(Equal([]envoy.FilterChain{
					{
						Filters: []envoy.Filter{
							{
								Name:   "envoy.tcp_proxy",
								Config: envoy.Config{StatPrefix: "0-stats", Cluster: "0-service-cluster"},
							},
						},
						TLSContext: envoy.TLSContext{
							CommonTLSContext: envoy.CommonTLSContext{
								TLSCertificateSDSSecretConfigs: []envoy.SecretConfig{
									{
										Name:      "server-cert-and-key",
										SDSConfig: envoy.SDSConfig{Path: "/etc/cf-assets/envoy_config/sds-server-cert-and-key.yaml"},
									},
								},
								TLSParams: envoy.TLSParams{
									CipherSuites: []string{"ECDHE-RSA-AES256-GCM-SHA384", "ECDHE-RSA-AES128-GCM-SHA256"},
								},
								ValidationContextSDSSecretConfig: envoy.SecretConfig{
									Name:      "server-validation-context",
									SDSConfig: envoy.SDSConfig{Path: "/etc/cf-assets/envoy_config/sds-server-validation-context.yaml"},
								},
							},
							RequireClientCertificate: true,
						},
					},
				}))

				listener = proxyConfig.StaticResources.Listeners[1]
				Expect(listener.Name).To(Equal("listener-2222"))
				Expect(listener.Address.SocketAddress.Address).To(Equal("0.0.0.0"))
				Expect(listener.Address.SocketAddress.PortValue).To(Equal(uint16(61002)))
				Expect(listener.FilterChains).To(Equal([]envoy.FilterChain{
					{
						Filters: []envoy.Filter{
							{
								Name:   "envoy.tcp_proxy",
								Config: envoy.Config{StatPrefix: "1-stats", Cluster: "1-service-cluster"},
							},
						},
						TLSContext: envoy.TLSContext{
							CommonTLSContext: envoy.CommonTLSContext{
								TLSCertificateSDSSecretConfigs: []envoy.SecretConfig{
									{
										Name:      "server-cert-and-key",
										SDSConfig: envoy.SDSConfig{Path: "/etc/cf-assets/envoy_config/sds-server-cert-and-key.yaml"},
									},
								},
								TLSParams: envoy.TLSParams{
									CipherSuites: []string{"ECDHE-RSA-AES256-GCM-SHA384", "ECDHE-RSA-AES128-GCM-SHA256"},
								},
								ValidationContextSDSSecretConfig: envoy.SecretConfig{
									Name:      "server-validation-context",
									SDSConfig: envoy.SDSConfig{Path: "/etc/cf-assets/envoy_config/sds-server-validation-context.yaml"},
								},
							},
							RequireClientCertificate: true,
						},
					},
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
					err := proxyConfigHandler.Update(containerstore.Credential{Cert: "cert", Key: "key"}, container)
					Expect(err).To(Equal(containerstore.ErrNoPortsAvailable))
				})
			})
		})
	})

	Describe("Close", func() {
		var (
			cert, key string
		)

		BeforeEach(func() {
			container.Ports = []executor.PortMapping{
				{
					ContainerPort:         8080,
					ContainerTLSProxyPort: 61001,
				},
			}

			cert, key, _ = generateCertAndKey()
		})

		JustBeforeEach(func() {
			_, _, err := proxyConfigHandler.CreateDir(logger, container)
			Expect(err).NotTo(HaveOccurred())
		})

		It("returns after the configured reload duration", func() {
			ch := make(chan struct{})
			go func() {
				proxyConfigHandler.Close(containerstore.Credential{Cert: cert, Key: key}, container)
				close(ch)
			}()

			Consistently(ch).ShouldNot(BeClosed())
			reloadClock.WaitForWatcherAndIncrement(1000 * time.Millisecond)
			Eventually(ch).Should(BeClosed())
		})

		Context("the EnableContainerProxy is disabled on the container", func() {
			BeforeEach(func() {
				container.EnableContainerProxy = false
			})

			It("does not write a envoy config file", func() {
				err := proxyConfigHandler.Update(containerstore.Credential{Cert: "", Key: ""}, container)
				Expect(err).NotTo(HaveOccurred())

				Expect(proxyConfigFile).NotTo(BeAnExistingFile())
			})

			It("doesn't wait until the proxy is serving the new cert", func() {
				ch := make(chan struct{})
				go func() {
					defer GinkgoRecover()
					proxyConfigHandler.Close(containerstore.Credential{Cert: cert, Key: key}, container)
					close(ch)
				}()

				Eventually(ch).Should(BeClosed())
			})
		})
	})
})

func generateCertAndKey() (string, string, *big.Int) {
	// generate a real cert
	privateKey, err := rsa.GenerateKey(rand.Reader, 512)
	Expect(err).ToNot(HaveOccurred())
	template := x509.Certificate{
		SerialNumber: new(big.Int),
		Subject:      pkix.Name{CommonName: "sample-cert"},
	}
	guid, err := uuid.NewV4()
	Expect(err).NotTo(HaveOccurred())

	guidBytes := [16]byte(*guid)
	template.SerialNumber.SetBytes(guidBytes[:])

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, privateKey.Public(), privateKey)

	privateKeyBytes := x509.MarshalPKCS1PrivateKey(privateKey)

	Expect(err).ToNot(HaveOccurred())
	cert := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes}))
	key := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: privateKeyBytes}))
	return cert, key, template.SerialNumber
}

func yamlFileToStruct(path string, outputStruct interface{}) error {
	yamlBytes, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}

	return yaml.Unmarshal(yamlBytes, outputStruct)
}
