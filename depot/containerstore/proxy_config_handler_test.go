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
	"runtime"
	"time"

	"code.cloudfoundry.org/clock/fakeclock"
	"code.cloudfoundry.org/executor"
	"code.cloudfoundry.org/executor/depot/containerstore"
	"code.cloudfoundry.org/garden"
	"code.cloudfoundry.org/lager/lagertest"
	envoy_bootstrap "github.com/envoyproxy/go-control-plane/envoy/config/bootstrap/v3"
	envoy_cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	envoy_core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	envoy_endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	envoy_listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	envoy_tcp_proxy "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/tcp_proxy/v3"
	envoy_tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	envoy_discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	envoy_matcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	"github.com/fsnotify/fsnotify"
	ghodss_yaml "github.com/ghodss/yaml"
	"github.com/gogo/protobuf/proto"
	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/duration"
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
		sdsIDCertAndKeyFile                string
		sdsC2CCertAndKeyFile               string
		sdsIDValidationContextFile         string
		proxyConfigFile                    string
		proxyConfigHandler                 *containerstore.ProxyConfigHandler
		reloadDuration                     time.Duration
		reloadClock                        *fakeclock.FakeClock
		containerProxyTrustedCACerts       []string
		containerProxyVerifySubjectAltName []string
		containerProxyRequireClientCerts   bool
		adsServers                         []string
		http2Enabled                       bool
		credentials                        containerstore.Credentials
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
		sdsIDCertAndKeyFile = filepath.Join(configPath, "sds-id-cert-and-key.yaml")
		sdsIDValidationContextFile = filepath.Join(configPath, "sds-id-validation-context.yaml")
		sdsC2CCertAndKeyFile = filepath.Join(configPath, "sds-c2c-cert-and-key.yaml")

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
		http2Enabled = true
		cert, key, _ := generateCertAndKey()
		credentials = containerstore.Credentials{
			InstanceIdentityCredential: containerstore.Credential{
				Cert: cert,
				Key:  key,
			},
			C2CCredential: containerstore.Credential{
				Cert: cert,
				Key:  key,
			},
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
			http2Enabled,
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
				err := proxyConfigHandler.Update(credentials, container)
				Expect(err).NotTo(HaveOccurred())
			})

			It("does nothing", func() {
				Expect(proxyConfigFile).NotTo(BeAnExistingFile())
			})
		})

		Describe("Update", func() {
			JustBeforeEach(func() {
				Expect(proxyConfigFile).NotTo(BeAnExistingFile())
				err := proxyConfigHandler.Update(credentials, container)
				Expect(err).NotTo(HaveOccurred())
			})

			It("does nothing", func() {
				Expect(proxyConfigFile).NotTo(BeAnExistingFile())
			})
		})

		It("returns an empty proxy port mapping", func() {
			ports, extraPorts, err := proxyConfigHandler.ProxyPorts(logger, &container)
			Expect(err).NotTo(HaveOccurred())
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
				{ContainerPort: 8080},
			}
		})

		Context("the EnableContainerProxy is disabled on the container", func() {
			BeforeEach(func() {
				container.EnableContainerProxy = false
			})

			It("returns an empty proxy port mapping", func() {
				ports, extraPorts, err := proxyConfigHandler.ProxyPorts(logger, &container)
				Expect(err).NotTo(HaveOccurred())
				Expect(ports).To(BeEmpty())
				Expect(extraPorts).To(BeEmpty())
			})
		})

		Context("when there is a default HTTP port (8080)", func() {
			BeforeEach(func() {
				container.Ports = []executor.PortMapping{
					{ContainerPort: 8080},
					{ContainerPort: 9090},
				}
			})

			It("each port gets an equivalent extra proxy port and additional 61443 port for 8080", func() {
				ports, extraPorts, err := proxyConfigHandler.ProxyPorts(logger, &container)
				Expect(err).NotTo(HaveOccurred())
				Expect(ports).To(ConsistOf([]executor.ProxyPortMapping{
					{
						AppPort:   8080,
						ProxyPort: 61001,
					},
					{
						AppPort:   8080,
						ProxyPort: 61443,
					},
					{
						AppPort:   9090,
						ProxyPort: 61002,
					},
				}))

				Expect(extraPorts).To(ConsistOf([]uint16{61001, 61002, 61443}))
			})
		})

		Context("when there is no default HTTP port (8080)", func() {
			BeforeEach(func() {
				container.Ports = []executor.PortMapping{
					{ContainerPort: 7070},
					{ContainerPort: 9090},
				}
			})

			It("each port gets an equivalent extra proxy port", func() {
				ports, extraPorts, err := proxyConfigHandler.ProxyPorts(logger, &container)
				Expect(err).NotTo(HaveOccurred())
				Expect(ports).To(ConsistOf([]executor.ProxyPortMapping{
					{
						AppPort:   7070,
						ProxyPort: 61001,
					},
					{
						AppPort:   9090,
						ProxyPort: 61002,
					},
				}))

				Expect(extraPorts).To(ConsistOf([]uint16{61001, 61002}))
			})
		})

		Context("when the requested ports are in the 6100n range", func() {
			BeforeEach(func() {
				container.Ports = []executor.PortMapping{
					{ContainerPort: 61001},
					{ContainerPort: 9090},
				}
			})

			It("the additional proxy ports don't collide with requested ports", func() {
				ports, extraPorts, err := proxyConfigHandler.ProxyPorts(logger, &container)
				Expect(err).NotTo(HaveOccurred())
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

		Context("when the requested port is 61443", func() {
			BeforeEach(func() {
				container.Ports = []executor.PortMapping{
					{ContainerPort: 61443},
				}
			})

			It("returns an error that 61443 is a reserved port", func() {
				_, _, err := proxyConfigHandler.ProxyPorts(logger, &container)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("port 61443 is reserved for container networking"))
			})
		})

		Context("when there are more than 443 ports", func() {
			BeforeEach(func() {
				container.Ports = []executor.PortMapping{}
				for i := 1; i < 445; i++ {
					container.Ports = append(container.Ports, executor.PortMapping{ContainerPort: uint16(i)})
				}
			})

			It("does not reserve port 61443", func() {
				ports, extraPorts, err := proxyConfigHandler.ProxyPorts(logger, &container)
				Expect(err).NotTo(HaveOccurred())
				Expect(ports[441]).To(Equal(executor.ProxyPortMapping{
					AppPort:   442,
					ProxyPort: 61442,
				}))
				Expect(ports[442]).To(Equal(executor.ProxyPortMapping{
					AppPort:   443,
					ProxyPort: 61444,
				}))
				Expect(extraPorts).NotTo(ContainElement(61443))
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
				{
					ContainerPort:         8080,
					ContainerTLSProxyPort: 61443,
				},
			}

			containerProxyRequireClientCerts = true
		})

		Context("the EnableContainerProxy is disabled on the container", func() {
			BeforeEach(func() {
				container.EnableContainerProxy = false
			})

			It("does not write a envoy config file", func() {
				err := proxyConfigHandler.Update(credentials, container)
				Expect(err).NotTo(HaveOccurred())

				Expect(proxyConfigFile).NotTo(BeAnExistingFile())
			})
		})

		Context("with containerProxyRequireClientCerts set to false", func() {
			BeforeEach(func() {
				containerProxyRequireClientCerts = false
			})

			It("creates a proxy config without tls_context", func() {
				err := proxyConfigHandler.Update(credentials, container)
				Expect(err).NotTo(HaveOccurred())

				Eventually(proxyConfigFile).Should(BeAnExistingFile())

				var proxyConfig envoy_bootstrap.Bootstrap
				Expect(yamlFileToProto(proxyConfigFile, &proxyConfig)).To(Succeed())

				admin := proxyConfig.Admin
				Expect(admin.AccessLogPath).To(Equal(os.DevNull))
				Expect(admin.Address).To(Equal(envoyAddr("127.0.0.1", 61002)))
				statsMatcher := proxyConfig.StatsConfig
				Expect(fmt.Sprintf("%+v", statsMatcher.StatsMatcher.StatsMatcher)).To(Equal("&{RejectAll:true}"))

				Expect(proxyConfig.Node.Id).To(Equal(fmt.Sprintf("sidecar~10.0.0.1~%s~x", container.Guid)))
				Expect(proxyConfig.Node.Cluster).To(Equal("proxy-cluster"))

				Expect(proxyConfig.StaticResources.Clusters).To(HaveLen(2))
				expectedCluster{
					name:           "service-cluster-8080",
					hosts:          []*envoy_core.Address{envoyAddr("10.0.0.1", 8080)},
					maxConnections: math.MaxUint32,
				}.check(proxyConfig.StaticResources.Clusters[0])

				adsCluster := proxyConfig.StaticResources.Clusters[1]
				expectedCluster{
					name: "pilot-ads",
					hosts: []*envoy_core.Address{
						envoyAddr("10.255.217.2", 15010),
						envoyAddr("10.255.217.3", 15010),
					}}.check(adsCluster)
				Expect(adsCluster.Http2ProtocolOptions).To(Equal(&envoy_core.Http2ProtocolOptions{}))

				Expect(proxyConfig.StaticResources.Listeners).To(HaveLen(2))
				expectedListener{
					name:                     "listener-8080-61001",
					listenPort:               61001,
					statPrefix:               "stats-8080-61001",
					clusterName:              "service-cluster-8080",
					requireClientCertificate: false,
					alpnProtocols:            []string{"h2,http/1.1"},
				}.check(proxyConfig.StaticResources.Listeners[0],
					"id-cert-and-key",
					"/etc/cf-assets/envoy_config/sds-id-cert-and-key.yaml",
				)
				expectedListener{
					name:                     "listener-8080-61443",
					listenPort:               61443,
					statPrefix:               "stats-8080-61443",
					clusterName:              "service-cluster-8080",
					requireClientCertificate: false,
					alpnProtocols:            []string{"h2,http/1.1"},
				}.check(proxyConfig.StaticResources.Listeners[1],
					"c2c-cert-and-key",
					"/etc/cf-assets/envoy_config/sds-c2c-cert-and-key.yaml",
				)
			})
		})

		It("creates appropriate sds-id-validation-context.yaml configuration file", func() {
			err := proxyConfigHandler.Update(credentials, container)
			Expect(err).NotTo(HaveOccurred())
			Eventually(sdsIDCertAndKeyFile).Should(BeAnExistingFile())
			Eventually(sdsC2CCertAndKeyFile).Should(BeAnExistingFile())

			var sdsCertificateDiscoveryResponse envoy_discovery.DiscoveryResponse
			Expect(yamlFileToProto(sdsIDCertAndKeyFile, &sdsCertificateDiscoveryResponse)).To(Succeed())

			Expect(sdsCertificateDiscoveryResponse.VersionInfo).To(Equal("0"))
			Expect(sdsCertificateDiscoveryResponse.Resources).To(HaveLen(1))

			var secret envoy_tls.Secret
			Expect(ptypes.UnmarshalAny(sdsCertificateDiscoveryResponse.Resources[0], &secret)).To(Succeed())

			Expect(secret.Name).To(Equal("id-cert-and-key"))
			Expect(secret.Type).To(Equal(&envoy_tls.Secret_TlsCertificate{
				TlsCertificate: &envoy_tls.TlsCertificate{
					CertificateChain: &envoy_core.DataSource{
						Specifier: &envoy_core.DataSource_InlineString{
							InlineString: credentials.InstanceIdentityCredential.Cert,
						},
					},
					PrivateKey: &envoy_core.DataSource{
						Specifier: &envoy_core.DataSource_InlineString{
							InlineString: credentials.InstanceIdentityCredential.Key,
						},
					},
				},
			}))
		})

		Context("when configuration files already exist", func() {
			var fileWatcher *fsnotify.Watcher

			BeforeEach(func() {
				if runtime.GOOS == "windows" {
					Skip("windows only generates a WRITE event regardless of just a write or rename")
				}

				var err error
				fileWatcher, err = fsnotify.NewWatcher()
				Expect(err).NotTo(HaveOccurred())
			})

			AfterEach(func() {
				Expect(fileWatcher.Close()).To(Succeed())
			})

			It("should ensure the sds-id-cert-and-key.yaml file is recreated when updating the config", func() {
				Expect(ioutil.WriteFile(sdsIDCertAndKeyFile, []byte("old-content"), 0666)).To(Succeed())
				Expect(fileWatcher.Add(sdsIDCertAndKeyFile)).To(Succeed())
				err := proxyConfigHandler.Update(credentials, container)
				Expect(err).NotTo(HaveOccurred())

				Eventually(fileWatcher.Events).Should(Receive(Equal(fsnotify.Event{Name: sdsIDCertAndKeyFile, Op: fsnotify.Remove})))
			})

			It("should ensure the sds-c2c-cert-and-key.yaml file is recreated when updating the config", func() {
				Expect(ioutil.WriteFile(sdsC2CCertAndKeyFile, []byte("old-content"), 0666)).To(Succeed())
				Expect(fileWatcher.Add(sdsC2CCertAndKeyFile)).To(Succeed())
				err := proxyConfigHandler.Update(credentials, container)
				Expect(err).NotTo(HaveOccurred())

				Eventually(fileWatcher.Events).Should(Receive(Equal(fsnotify.Event{Name: sdsC2CCertAndKeyFile, Op: fsnotify.Remove})))
			})

			It("should ensure the sds-id-validation-context.yaml file is recreated when updating the config", func() {
				Expect(ioutil.WriteFile(sdsIDValidationContextFile, []byte("old-content"), 0666)).To(Succeed())
				Expect(fileWatcher.Add(sdsIDValidationContextFile)).To(Succeed())
				err := proxyConfigHandler.Update(credentials, container)
				Expect(err).NotTo(HaveOccurred())

				Eventually(fileWatcher.Events).Should(Receive(Equal(fsnotify.Event{Name: sdsIDValidationContextFile, Op: fsnotify.Remove})))
			})
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

			It("creates appropriate sds-id-validation-context.yaml configuration file", func() {
				err := proxyConfigHandler.Update(credentials, container)
				Expect(err).NotTo(HaveOccurred())
				Eventually(sdsIDValidationContextFile).Should(BeAnExistingFile())

				var sdsDiscoveryResponse envoy_discovery.DiscoveryResponse
				Expect(yamlFileToProto(sdsIDValidationContextFile, &sdsDiscoveryResponse)).To(Succeed())

				Expect(sdsDiscoveryResponse.VersionInfo).To(Equal("0"))
				Expect(sdsDiscoveryResponse.Resources).To(HaveLen(1))

				var secret envoy_tls.Secret
				Expect(ptypes.UnmarshalAny(sdsDiscoveryResponse.Resources[0], &secret)).To(Succeed())

				Expect(secret.Name).To(Equal("id-validation-context"))
				Expect(secret.Type).To(Equal(&envoy_tls.Secret_ValidationContext{
					ValidationContext: &envoy_tls.CertificateValidationContext{
						TrustedCa: &envoy_core.DataSource{
							Specifier: &envoy_core.DataSource_InlineString{
								InlineString: inlinedCert,
							},
						},
						MatchSubjectAltNames: []*envoy_matcher.StringMatcher{
							{MatchPattern: &envoy_matcher.StringMatcher_Exact{Exact: "valid-alt-name-1"}},
							{MatchPattern: &envoy_matcher.StringMatcher_Exact{Exact: "valid-alt-name-2"}},
						},
					},
				}))
			})
		})

		It("creates the appropriate proxy config at start", func() {
			err := proxyConfigHandler.Update(credentials, container)
			Expect(err).NotTo(HaveOccurred())

			Eventually(proxyConfigFile).Should(BeAnExistingFile())

			var proxyConfig envoy_bootstrap.Bootstrap
			Expect(yamlFileToProto(proxyConfigFile, &proxyConfig)).To(Succeed())

			admin := proxyConfig.Admin
			Expect(admin.AccessLogPath).To(Equal(os.DevNull))
			Expect(admin.Address).To(Equal(envoyAddr("127.0.0.1", 61002)))
			statsMatcher := proxyConfig.StatsConfig
			Expect(fmt.Sprintf("%+v", statsMatcher.StatsMatcher.StatsMatcher)).To(Equal("&{RejectAll:true}"))

			Expect(proxyConfig.Node.Id).To(Equal(fmt.Sprintf("sidecar~10.0.0.1~%s~x", container.Guid)))
			Expect(proxyConfig.Node.Cluster).To(Equal("proxy-cluster"))

			Expect(proxyConfig.StaticResources.Clusters).To(HaveLen(2))
			expectedCluster{
				name:           "service-cluster-8080",
				hosts:          []*envoy_core.Address{envoyAddr("10.0.0.1", 8080)},
				maxConnections: math.MaxUint32,
			}.check(proxyConfig.StaticResources.Clusters[0])

			adsCluster := proxyConfig.StaticResources.Clusters[1]
			expectedCluster{
				name: "pilot-ads",
				hosts: []*envoy_core.Address{
					envoyAddr("10.255.217.2", 15010),
					envoyAddr("10.255.217.3", 15010),
				}}.check(adsCluster)
			Expect(adsCluster.Http2ProtocolOptions).To(Equal(&envoy_core.Http2ProtocolOptions{}))

			Expect(proxyConfig.StaticResources.Listeners).To(HaveLen(2))
			expectedListener{
				name:                     "listener-8080-61001",
				listenPort:               61001,
				statPrefix:               "stats-8080-61001",
				clusterName:              "service-cluster-8080",
				requireClientCertificate: true,
				alpnProtocols:            []string{"h2,http/1.1"},
			}.check(proxyConfig.StaticResources.Listeners[0],
				"id-cert-and-key",
				"/etc/cf-assets/envoy_config/sds-id-cert-and-key.yaml",
			)
			expectedListener{
				name:                     "listener-8080-61443",
				listenPort:               61443,
				statPrefix:               "stats-8080-61443",
				clusterName:              "service-cluster-8080",
				requireClientCertificate: false,
				alpnProtocols:            []string{"h2,http/1.1"},
			}.check(proxyConfig.StaticResources.Listeners[1],
				"c2c-cert-and-key",
				"/etc/cf-assets/envoy_config/sds-c2c-cert-and-key.yaml",
			)

			adsConfigSource := &envoy_core.ConfigSource{
				ConfigSourceSpecifier: &envoy_core.ConfigSource_Ads{
					Ads: &envoy_core.AggregatedConfigSource{},
				},
			}

			Expect(proxyConfig.DynamicResources).To(Equal(&envoy_bootstrap.Bootstrap_DynamicResources{
				LdsConfig: adsConfigSource,
				CdsConfig: adsConfigSource,
				AdsConfig: &envoy_core.ApiConfigSource{
					ApiType: envoy_core.ApiConfigSource_GRPC,
					GrpcServices: []*envoy_core.GrpcService{
						{
							TargetSpecifier: &envoy_core.GrpcService_EnvoyGrpc_{
								EnvoyGrpc: &envoy_core.GrpcService_EnvoyGrpc{
									ClusterName: "pilot-ads",
								},
							},
						},
					},
				},
			}))
		})

		Context("when ads server addresses is empty", func() {
			BeforeEach(func() {
				adsServers = []string{}
			})

			It("creates a proxy config without the ads config", func() {
				err := proxyConfigHandler.Update(credentials, container)
				Expect(err).NotTo(HaveOccurred())
				Eventually(proxyConfigFile).Should(BeAnExistingFile())

				var proxyConfig envoy_bootstrap.Bootstrap
				Expect(yamlFileToProto(proxyConfigFile, &proxyConfig)).To(Succeed())

				Expect(proxyConfig.StaticResources.Clusters).To(HaveLen(1))
				cluster := proxyConfig.StaticResources.Clusters[0]
				Expect(cluster.Name).To(Equal("service-cluster-8080"))

				Expect(proxyConfig.DynamicResources).To(BeNil())
			})
		})

		Context("when an ads server address is malformed", func() {
			BeforeEach(func() {
				adsServers = []string{
					"malformed",
				}
			})

			It("returns an error", func() {
				err := proxyConfigHandler.Update(credentials, container)
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
				err := proxyConfigHandler.Update(credentials, container)
				Expect(err).To(MatchError("ads server address is invalid: 0.0.0.0:mal"))
			})
		})

		Context("when HTTP/2 is disabled", func() {
			BeforeEach(func() {
				http2Enabled = false
			})

			It("creates a proxy config without ALPN for listeners", func() {
				err := proxyConfigHandler.Update(credentials, container)
				Expect(err).NotTo(HaveOccurred())
				Eventually(proxyConfigFile).Should(BeAnExistingFile())

				var proxyConfig envoy_bootstrap.Bootstrap
				Expect(yamlFileToProto(proxyConfigFile, &proxyConfig)).To(Succeed())

				Expect(proxyConfig.StaticResources.Listeners).To(HaveLen(2))
				expectedListener{
					name:                     "listener-8080-61001",
					listenPort:               61001,
					statPrefix:               "stats-8080-61001",
					clusterName:              "service-cluster-8080",
					requireClientCertificate: true,
					alpnProtocols:            nil,
				}.check(proxyConfig.StaticResources.Listeners[0],
					"id-cert-and-key",
					"/etc/cf-assets/envoy_config/sds-id-cert-and-key.yaml",
				)
				expectedListener{
					name:                     "listener-8080-61443",
					listenPort:               61443,
					statPrefix:               "stats-8080-61443",
					clusterName:              "service-cluster-8080",
					requireClientCertificate: false,
					alpnProtocols:            nil,
				}.check(proxyConfig.StaticResources.Listeners[1],
					"c2c-cert-and-key",
					"/etc/cf-assets/envoy_config/sds-c2c-cert-and-key.yaml",
				)
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
				err := proxyConfigHandler.Update(credentials, container)
				Expect(err).NotTo(HaveOccurred())
				Eventually(proxyConfigFile).Should(BeAnExistingFile())

				var proxyConfig envoy_bootstrap.Bootstrap
				Expect(yamlFileToProto(proxyConfigFile, &proxyConfig)).To(Succeed())

				admin := proxyConfig.Admin
				Expect(admin.AccessLogPath).To(Equal(os.DevNull))
				Expect(admin.Address).To(Equal(envoyAddr("127.0.0.1", 61003)))
				statsMatcher := proxyConfig.StatsConfig
				Expect(fmt.Sprintf("%+v", statsMatcher.StatsMatcher.StatsMatcher)).To(Equal("&{RejectAll:true}"))

				Expect(proxyConfig.Node.Id).To(Equal(fmt.Sprintf("sidecar~10.0.0.1~%s~x", container.Guid)))
				Expect(proxyConfig.Node.Cluster).To(Equal("proxy-cluster"))

				Expect(proxyConfig.StaticResources.Clusters).To(HaveLen(3))

				expectedCluster{
					name:           "service-cluster-8080",
					hosts:          []*envoy_core.Address{envoyAddr("10.0.0.1", 8080)},
					maxConnections: math.MaxUint32,
				}.check(proxyConfig.StaticResources.Clusters[0])

				expectedCluster{
					name:           "service-cluster-2222",
					hosts:          []*envoy_core.Address{envoyAddr("10.0.0.1", 2222)},
					maxConnections: math.MaxUint32,
				}.check(proxyConfig.StaticResources.Clusters[1])

				adsCluster := proxyConfig.StaticResources.Clusters[2]
				expectedCluster{
					name: "pilot-ads",
					hosts: []*envoy_core.Address{
						envoyAddr("10.255.217.2", 15010),
						envoyAddr("10.255.217.3", 15010),
					},
				}.check(adsCluster)
				Expect(adsCluster.Http2ProtocolOptions).To(Equal(&envoy_core.Http2ProtocolOptions{}))

				Expect(proxyConfig.StaticResources.Listeners).To(HaveLen(2))
				expectedListener{
					name:                     "listener-8080-61001",
					listenPort:               61001,
					statPrefix:               "stats-8080-61001",
					clusterName:              "service-cluster-8080",
					requireClientCertificate: true,
					alpnProtocols:            []string{"h2,http/1.1"},
				}.check(proxyConfig.StaticResources.Listeners[0],
					"id-cert-and-key",
					"/etc/cf-assets/envoy_config/sds-id-cert-and-key.yaml",
				)

				expectedListener{
					name:                     "listener-2222-61002",
					listenPort:               61002,
					statPrefix:               "stats-2222-61002",
					clusterName:              "service-cluster-2222",
					requireClientCertificate: true,
					alpnProtocols:            []string{"h2,http/1.1"},
				}.check(proxyConfig.StaticResources.Listeners[1],
					"id-cert-and-key",
					"/etc/cf-assets/envoy_config/sds-id-cert-and-key.yaml",
				)
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
					err := proxyConfigHandler.Update(credentials, container)
					Expect(err).To(Equal(containerstore.ErrNoPortsAvailable))
				})
			})
		})
	})

	Describe("Close", func() {
		BeforeEach(func() {
			container.Ports = []executor.PortMapping{
				{
					ContainerPort:         8080,
					ContainerTLSProxyPort: 61001,
				},
			}
		})

		JustBeforeEach(func() {
			_, _, err := proxyConfigHandler.CreateDir(logger, container)
			Expect(err).NotTo(HaveOccurred())
		})

		It("returns after the configured reload duration", func() {
			ch := make(chan struct{})
			go func() {
				proxyConfigHandler.Close(credentials, container)
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
				err := proxyConfigHandler.Update(credentials, container)
				Expect(err).NotTo(HaveOccurred())

				Expect(proxyConfigFile).NotTo(BeAnExistingFile())
			})

			It("doesn't wait until the proxy is serving the new cert", func() {
				ch := make(chan struct{})
				go func() {
					defer GinkgoRecover()
					proxyConfigHandler.Close(credentials, container)
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

func yamlFileToProto(path string, outputProto proto.Message) error {
	yamlBytes, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}

	jsonBytes, err := ghodss_yaml.YAMLToJSON(yamlBytes)
	if err != nil {
		return err
	}

	err = jsonpb.UnmarshalString(string(jsonBytes), outputProto)
	if err != nil {
		return err
	}

	// Check if unmarshalled proto follows validation rules
	return outputProto.(interface{ Validate() error }).Validate()
}

func envoyAddr(ip string, port int) *envoy_core.Address {
	return &envoy_core.Address{
		Address: &envoy_core.Address_SocketAddress{
			SocketAddress: &envoy_core.SocketAddress{
				Address: ip,
				PortSpecifier: &envoy_core.SocketAddress_PortValue{
					PortValue: uint32(port),
				},
			},
		},
	}
}

type expectedListener struct {
	name                     string
	listenPort               int
	statPrefix               string
	clusterName              string
	requireClientCertificate bool
	alpnProtocols            []string
}

func (l expectedListener) check(listener *envoy_listener.Listener, sdsSecretName string, sdsFileName string) {
	Expect(listener.Name).To(Equal(l.name))
	Expect(listener.Address).To(Equal(envoyAddr("0.0.0.0", l.listenPort)))
	Expect(listener.FilterChains).To(HaveLen(1))
	filterChain := listener.FilterChains[0]
	Expect(filterChain.Filters).To(HaveLen(1))
	Expect(filterChain.Filters[0].Name).To(Equal("envoy.tcp_proxy"))
	filterConfig := filterChain.Filters[0].GetTypedConfig()

	var tcpProxyFilterConfig envoy_tcp_proxy.TcpProxy
	Expect(ptypes.UnmarshalAny(filterConfig, &tcpProxyFilterConfig)).To(Succeed())
	Expect(tcpProxyFilterConfig.StatPrefix).To(Equal(l.statPrefix))
	Expect(tcpProxyFilterConfig.ClusterSpecifier).To(Equal(&envoy_tcp_proxy.TcpProxy_Cluster{
		Cluster: l.clusterName,
	}))

	var downstreamTlsContext envoy_tls.DownstreamTlsContext
	Expect(ptypes.UnmarshalAny(filterChain.TransportSocket.GetTypedConfig(), &downstreamTlsContext)).To(Succeed())
	Expect(filterChain.TransportSocket.Name).To(Equal(l.name))

	Expect(downstreamTlsContext.CommonTlsContext.AlpnProtocols).To(Equal(l.alpnProtocols))
	Expect(downstreamTlsContext.CommonTlsContext.TlsCertificateSdsSecretConfigs).To(ConsistOf(
		&envoy_tls.SdsSecretConfig{
			Name: sdsSecretName,
			SdsConfig: &envoy_core.ConfigSource{
				ConfigSourceSpecifier: &envoy_core.ConfigSource_Path{
					Path: sdsFileName,
				},
			},
		},
	))
	Expect(downstreamTlsContext.CommonTlsContext.TlsParams).To(Equal(&envoy_tls.TlsParameters{
		CipherSuites: []string{"ECDHE-RSA-AES256-GCM-SHA384", "ECDHE-RSA-AES128-GCM-SHA256"},
	}))

	if l.requireClientCertificate {
		Expect(downstreamTlsContext.RequireClientCertificate.Value).To(BeTrue())
		Expect(downstreamTlsContext.CommonTlsContext.ValidationContextType).To(Equal(&envoy_tls.CommonTlsContext_ValidationContextSdsSecretConfig{
			ValidationContextSdsSecretConfig: &envoy_tls.SdsSecretConfig{
				Name: "id-validation-context",
				SdsConfig: &envoy_core.ConfigSource{
					ConfigSourceSpecifier: &envoy_core.ConfigSource_Path{
						Path: "/etc/cf-assets/envoy_config/sds-id-validation-context.yaml",
					},
				},
			},
		}))
	} else {
		Expect(downstreamTlsContext.CommonTlsContext.ValidationContextType).To(BeNil())
	}
}

type expectedCluster struct {
	name           string
	hosts          []*envoy_core.Address
	maxConnections uint32
}

func (c expectedCluster) check(cluster *envoy_cluster.Cluster) {
	Expect(cluster.Name).To(Equal(c.name))
	Expect(cluster.ConnectTimeout).To(Equal(&duration.Duration{Nanos: 250000000}))
	Expect(cluster.ClusterDiscoveryType).To(Equal(&envoy_cluster.Cluster_Type{Type: envoy_cluster.Cluster_STATIC}))
	Expect(cluster.LbPolicy).To(Equal(envoy_cluster.Cluster_ROUND_ROBIN))
	var expectedLbEndpoints []*envoy_endpoint.LbEndpoint
	for _, h := range c.hosts {
		expectedLbEndpoints = append(expectedLbEndpoints, &envoy_endpoint.LbEndpoint{
			HostIdentifier: &envoy_endpoint.LbEndpoint_Endpoint{
				Endpoint: &envoy_endpoint.Endpoint{
					Address: h,
				},
			},
		})
	}
	Expect(cluster.LoadAssignment).To(Equal(&envoy_endpoint.ClusterLoadAssignment{
		ClusterName: c.name,
		Endpoints: []*envoy_endpoint.LocalityLbEndpoints{{
			LbEndpoints: expectedLbEndpoints,
		}},
	}))
	if c.maxConnections > 0 {
		Expect(cluster.CircuitBreakers.Thresholds).To(HaveLen(1))
		Expect(cluster.CircuitBreakers.Thresholds[0].MaxConnections.Value).To(BeNumerically("==", c.maxConnections))
	} else {
		Expect(cluster.CircuitBreakers).To(BeNil())
	}
}
