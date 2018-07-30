package containerstore

import (
	"bytes"
	"encoding/pem"
	"errors"
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/tedsuo/ifrit"
	yaml "gopkg.in/yaml.v2"

	"code.cloudfoundry.org/executor"
	"code.cloudfoundry.org/executor/depot/containerstore/envoy"
	"code.cloudfoundry.org/garden"
	"code.cloudfoundry.org/lager"
)

const (
	StartProxyPort = 61001
	EndProxyPort   = 65534

	TimeOut    = "0.25s"
	Static     = "STATIC"
	RoundRobin = "ROUND_ROBIN"

	IngressListener = "ingress_listener"
	TcpProxy        = "envoy.tcp_proxy"

	AdminAccessLog = "/dev/null"
)

var (
	ErrNoPortsAvailable = errors.New("no ports available")

	SupportedCipherSuites = "[ECDHE-RSA-AES256-GCM-SHA384|ECDHE-RSA-AES128-GCM-SHA256]"
)

var dummyRunner = func(credRotatedChan <-chan Credential) ifrit.Runner {
	return ifrit.RunFunc(func(signals <-chan os.Signal, ready chan<- struct{}) error {
		close(ready)
		for {
			select {
			case <-credRotatedChan:
			case <-signals:
				return nil
			}
		}
	})
}

//go:generate counterfeiter -o containerstorefakes/fake_proxymanager.go . ProxyManager
type ProxyManager interface {
	ProxyPorts(lager.Logger, *executor.Container) ([]executor.ProxyPortMapping, []uint16)
	BindMounts(lager.Logger, executor.Container) ([]garden.BindMount, error)
	RemoveProxyConfigDir(lager.Logger, executor.Container) error
	Runner(lager.Logger, executor.Container, <-chan Credential) (ProxyRunner, error)
}

//go:generate counterfeiter -o containerstorefakes/fake_proxyrunner.go . ProxyRunner
type ProxyRunner interface {
	ifrit.Runner
	Port() uint16
}

type proxyRunner struct {
	ifrit.Runner
	ldsPort uint16
}

type proxyManager struct {
	logger                             lager.Logger
	containerProxyPath                 string
	containerProxyConfigPath           string
	containerProxyTrustedCACerts       []string
	containerProxyVerifySubjectAltName []string
	containerProxyRequireClientCerts   bool

	refreshDelayMS time.Duration
}

type noopProxyManager struct{}

func (p *noopProxyManager) BindMounts(logger lager.Logger, container executor.Container) ([]garden.BindMount, error) {
	return []garden.BindMount{}, nil
}

func (p *noopProxyManager) RemoveProxyConfigDir(logger lager.Logger, container executor.Container) error {
	return nil
}

func (p *noopProxyManager) ProxyPorts(lager.Logger, *executor.Container) ([]executor.ProxyPortMapping, []uint16) {
	return nil, nil
}

func (p *noopProxyManager) Runner(logger lager.Logger, container executor.Container, credRotatedChan <-chan Credential) (ProxyRunner, error) {
	return &proxyRunner{
		Runner: dummyRunner(credRotatedChan),
	}, nil
}

func NewNoopProxyManager() ProxyManager {
	return &noopProxyManager{}
}

func NewProxyManager(
	logger lager.Logger,
	containerProxyPath string,
	containerProxyConfigPath string,
	ContainerProxyTrustedCACerts []string,
	ContainerProxyVerifySubjectAltName []string,
	containerProxyRequireClientCerts bool,
	refreshDelayMS time.Duration,
) ProxyManager {
	return &proxyManager{
		logger:                             logger.Session("proxy-manager"),
		containerProxyPath:                 containerProxyPath,
		containerProxyConfigPath:           containerProxyConfigPath,
		containerProxyTrustedCACerts:       ContainerProxyTrustedCACerts,
		containerProxyVerifySubjectAltName: ContainerProxyVerifySubjectAltName,
		containerProxyRequireClientCerts:   containerProxyRequireClientCerts,
		refreshDelayMS:                     refreshDelayMS,
	}
}

func (p *proxyManager) BindMounts(logger lager.Logger, container executor.Container) ([]garden.BindMount, error) {
	if !container.EnableContainerProxy {
		return nil, nil
	}

	logger.Info("adding-container-proxy-bindmounts")
	proxyConfigDir := filepath.Join(p.containerProxyConfigPath, container.Guid)
	mounts := []garden.BindMount{
		{
			Origin:  garden.BindMountOriginHost,
			SrcPath: p.containerProxyPath,
			DstPath: "/etc/cf-assets/envoy",
		},
		{
			Origin:  garden.BindMountOriginHost,
			SrcPath: proxyConfigDir,
			DstPath: "/etc/cf-assets/envoy_config",
		},
	}

	err := os.MkdirAll(proxyConfigDir, 0755)
	if err != nil {
		return nil, err
	}
	return mounts, nil
}

func (p *proxyManager) RemoveProxyConfigDir(logger lager.Logger, container executor.Container) error {
	logger.Info("removing-container-proxy-config-dir")
	proxyConfigDir := filepath.Join(p.containerProxyConfigPath, container.Guid)
	return os.RemoveAll(proxyConfigDir)
}

// This modifies the container pointer in order to create garden NetIn rules in the storenode.Create
func (p *proxyManager) ProxyPorts(logger lager.Logger, container *executor.Container) ([]executor.ProxyPortMapping, []uint16) {
	if !container.EnableContainerProxy {
		return nil, nil
	}

	proxyPortMapping := []executor.ProxyPortMapping{}

	existingPorts := make(map[uint16]interface{})
	containerPorts := make([]uint16, len(container.Ports))
	for i, portMap := range container.Ports {
		existingPorts[portMap.ContainerPort] = struct{}{}
		containerPorts[i] = portMap.ContainerPort
	}

	extraPorts := []uint16{}

	portCount := 0
	for port := uint16(StartProxyPort); port < EndProxyPort; port++ {
		if portCount == len(existingPorts) {
			break
		}

		if existingPorts[port] != nil {
			continue
		}

		extraPorts = append(extraPorts, port)
		proxyPortMapping = append(proxyPortMapping, executor.ProxyPortMapping{
			AppPort:   containerPorts[portCount],
			ProxyPort: port,
		})

		portCount++
	}

	return proxyPortMapping, extraPorts
}

func (p *proxyManager) Runner(logger lager.Logger, container executor.Container, credRotatedChan <-chan Credential) (ProxyRunner, error) {
	if !container.EnableContainerProxy {
		return &proxyRunner{
			Runner: dummyRunner(credRotatedChan),
		}, nil
	}

	port, err := getAvailablePort(container.Ports)
	if err != nil {
		return nil, err
	}

	adminPort, err := getAvailablePort(container.Ports, port)
	if err != nil {
		return nil, err
	}

	proxyRunner := &proxyRunner{
		Runner: ifrit.RunFunc(func(signals <-chan os.Signal, ready chan<- struct{}) error {
			logger = logger.Session("proxy-manager")
			logger.Info("starting")
			defer logger.Info("finished")

			proxyConfigPath := filepath.Join(p.containerProxyConfigPath, container.Guid, "envoy.yaml")
			listenerConfigPath := filepath.Join(p.containerProxyConfigPath, container.Guid, "listeners.yaml")

			proxyConfig, err := generateProxyConfig(logger, container, p.refreshDelayMS, port, adminPort)
			if err != nil {
				logger.Error("failed-generating-proxy-config", err)
				return err
			}

			err = writeProxyConfig(proxyConfig, proxyConfigPath)
			if err != nil {
				logger.Error("failed-writing-initial-proxy-listener-config", err)
				return err
			}

			select {
			case creds := <-credRotatedChan:
				logger = logger.Session("updating-proxy-listener-config")
				logger.Debug("started")

				listenerConfig, err := generateListenerConfig(logger, container, creds, p.containerProxyTrustedCACerts, p.containerProxyVerifySubjectAltName, p.containerProxyRequireClientCerts)
				if err != nil {
					logger.Error("failed-generating-initial-proxy-listener-config", err)
					return err
				}

				err = writeListenerConfig(listenerConfig, listenerConfigPath)
				if err != nil {
					logger.Error("failed-writing-initial-proxy-listener-config", err)
					return err
				}
				logger.Debug("completed")
			case signal := <-signals:
				logger.Info("signaled", lager.Data{"signal": signal.String()})
				configPath := filepath.Join(p.containerProxyConfigPath, container.Guid)
				p.logger.Info("cleanup-proxy-config-path", lager.Data{"config-path": configPath})
				return os.RemoveAll(configPath)
			}

			close(ready)
			logger.Info("started")

			for {
				creds, ok := <-credRotatedChan
				if !ok {
					break
				}
				logger = logger.Session("updating-proxy-listener-config")
				logger.Debug("started")

				listenerConfig, err := generateListenerConfig(logger, container, creds, p.containerProxyTrustedCACerts, p.containerProxyVerifySubjectAltName, p.containerProxyRequireClientCerts)
				if err != nil {
					logger.Error("failed-generating-proxy-listener-config", err)
					return err
				}

				err = writeListenerConfig(listenerConfig, listenerConfigPath)
				if err != nil {
					logger.Error("failed-writing-proxy-listener-config", err)
					return err
				}
				logger.Debug("completed")
			}

			// time.Sleep(2 * time.Second)

			signal := <-signals
			logger.Info("signaled", lager.Data{"signal": signal.String()})
			configPath := filepath.Join(p.containerProxyConfigPath, container.Guid)
			p.logger.Info("cleanup-proxy-config-path", lager.Data{"config-path": configPath})
			// return os.RemoveAll(configPath)
			return nil
		}),
		ldsPort: port}
	return proxyRunner, nil
}

func (p *proxyRunner) Port() uint16 {
	return p.ldsPort
}

func generateProxyConfig(logger lager.Logger, container executor.Container, refreshDelayMS time.Duration, port, adminPort uint16) (envoy.ProxyConfig, error) {
	clusters := []envoy.Cluster{}
	for index, portMap := range container.Ports {
		clusterName := fmt.Sprintf("%d-service-cluster", index)
		clusters = append(clusters, envoy.Cluster{
			Name:              clusterName,
			ConnectionTimeout: TimeOut,
			Type:              Static,
			LbPolicy:          RoundRobin,
			Hosts: []envoy.Address{
				{SocketAddress: envoy.SocketAddress{Address: container.InternalIP, PortValue: portMap.ContainerPort}},
			},
			CircuitBreakers: envoy.CircuitBreakers{Thresholds: []envoy.Threshold{
				{MaxConnections: math.MaxUint32},
			}},
		})
	}

	config := envoy.ProxyConfig{
		Admin: envoy.Admin{
			AccessLogPath: AdminAccessLog,
			Address: envoy.Address{
				SocketAddress: envoy.SocketAddress{
					Address:   "127.0.0.1",
					PortValue: adminPort,
				},
			},
		},
		StaticResources: envoy.StaticResources{
			Clusters: clusters,
		},
		DynamicResources: envoy.DynamicResources{
			LDSConfig: envoy.LDSConfig{
				Path: "/etc/cf-assets/envoy_config/listeners.yaml",
			},
		},
	}
	return config, nil
}

func writeProxyConfig(proxyConfig envoy.ProxyConfig, path string) error {
	data, err := yaml.Marshal(proxyConfig)
	if err != nil {
		return err
	}

	return ioutil.WriteFile(path, data, 0666)
}

func writeListenerConfig(listenerConfig envoy.ListenerConfig, path string) error {
	tmpPath := path + ".tmp"

	data, err := yaml.Marshal(listenerConfig)
	if err != nil {
		return err
	}

	err = ioutil.WriteFile(tmpPath, data, 0666)
	if err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func generateListenerConfig(logger lager.Logger, container executor.Container, creds Credential, trustedCaCerts []string, subjectAltNames []string, requireClientCerts bool) (envoy.ListenerConfig, error) {
	resources := []envoy.Resource{}

	if !requireClientCerts {
		subjectAltNames = nil
	}

	var certs string
	var err error

	if requireClientCerts {
		certs, err = pemConcatenate(trustedCaCerts)
		if err != nil {
			return envoy.ListenerConfig{}, err
		}
	}

	for index, portMap := range container.Ports {
		listenerName := TcpProxy
		clusterName := fmt.Sprintf("%d-service-cluster", index)

		resources = append(resources, envoy.Resource{
			Type:    "type.googleapis.com/envoy.api.v2.Listener",
			Name:    fmt.Sprintf("listener-%d", portMap.ContainerPort),
			Address: envoy.Address{SocketAddress: envoy.SocketAddress{Address: "0.0.0.0", PortValue: portMap.ContainerTLSProxyPort}},
			FilterChains: []envoy.FilterChain{envoy.FilterChain{
				Filters: []envoy.Filter{
					envoy.Filter{
						Name: listenerName,
						Config: envoy.Config{
							StatPrefix: fmt.Sprintf("%d-stats", index),
							Cluster:    clusterName,
							AccessLogs: []envoy.AccessLog{
								{
									Name: "envoy.file_access_log",
									Config: envoy.AccessLogConfig{
										Path: "/dev/stdout",
									},
								},
							},
						},
					},
				},
				TLSContext: envoy.TLSContext{
					RequireClientCertificate: requireClientCerts,
					CommonTLSContext: envoy.CommonTLSContext{
						TLSParams: envoy.TLSParams{
							CipherSuites: SupportedCipherSuites,
						},
						TLSCertificates: []envoy.TLSCertificate{
							envoy.TLSCertificate{
								CertificateChain: envoy.DataSource{InlineString: creds.Cert},
								PrivateKey:       envoy.DataSource{InlineString: creds.Key},
							},
						},
						ValidationContext: envoy.CertificateValidationContext{
							TrustedCA:            envoy.DataSource{InlineString: certs},
							VerifySubjectAltName: subjectAltNames,
						},
					},
				},
			},
			},
		})
	}

	config := envoy.ListenerConfig{
		VersionInfo: "0",
		Resources:   resources,
	}

	return config, nil
}

func pemConcatenate(certs []string) (string, error) {
	var certificateBuf bytes.Buffer
	for _, cert := range certs {
		block, _ := pem.Decode([]byte(cert))
		if block == nil {
			return "", errors.New("failed to read certificate.")
		}
		pem.Encode(&certificateBuf, block)
	}
	return certificateBuf.String(), nil
}

func getAvailablePort(allocatedPorts []executor.PortMapping, extraKnownPorts ...uint16) (uint16, error) {
	existingPorts := make(map[uint16]interface{})
	for _, portMap := range allocatedPorts {
		existingPorts[portMap.ContainerPort] = struct{}{}
		existingPorts[portMap.ContainerTLSProxyPort] = struct{}{}
	}

	for _, extraKnownPort := range extraKnownPorts {
		existingPorts[extraKnownPort] = struct{}{}
	}

	for port := uint16(StartProxyPort); port < EndProxyPort; port++ {
		if existingPorts[port] != nil {
			continue
		}
		return port, nil
	}
	return 0, ErrNoPortsAvailable
}
