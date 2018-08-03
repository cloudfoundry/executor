package containerstore

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io/ioutil"
	"math"
	"math/big"
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
	ErrNoPortsAvailable   = errors.New("no ports available")
	ErrInvalidCertificate = errors.New("cannot parse invalid certificate")

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

type ProxyConfigHandler struct {
	logger                             lager.Logger
	containerProxyPath                 string
	containerProxyConfigPath           string
	containerProxyTrustedCACerts       []string
	containerProxyVerifySubjectAltName []string
	containerProxyRequireClientCerts   bool

	refreshDelayMS time.Duration
}

type NoopProxyConfigHandler struct{}

func (p *NoopProxyConfigHandler) CreateDir(logger lager.Logger, container executor.Container) ([]garden.BindMount, []executor.EnvironmentVariable, error) {
	return nil, nil, nil
}
func (p *NoopProxyConfigHandler) RemoveDir(logger lager.Logger, container executor.Container) error {
	return nil
}
func (p *NoopProxyConfigHandler) Update(credentials Credential, container executor.Container) error {
	return nil
}
func (p *NoopProxyConfigHandler) Close(invalidCredentials Credential, container executor.Container) error {
	return nil
}

func (p *NoopProxyConfigHandler) RemoveProxyConfigDir(logger lager.Logger, container executor.Container) error {
	return nil
}

func (p *NoopProxyConfigHandler) ProxyPorts(lager.Logger, *executor.Container) ([]executor.ProxyPortMapping, []uint16) {
	return nil, nil
}

func (p *NoopProxyConfigHandler) Runner(logger lager.Logger, container executor.Container, credRotatedChan <-chan Credential) (ifrit.Runner, error) {
	return dummyRunner(credRotatedChan), nil
}

func NewNoopProxyConfigHandler() *NoopProxyConfigHandler {
	return &NoopProxyConfigHandler{}
}

func NewProxyConfigHandler(
	logger lager.Logger,
	containerProxyPath string,
	containerProxyConfigPath string,
	ContainerProxyTrustedCACerts []string,
	ContainerProxyVerifySubjectAltName []string,
	containerProxyRequireClientCerts bool,
	refreshDelayMS time.Duration,
) *ProxyConfigHandler {
	return &ProxyConfigHandler{
		logger:                             logger.Session("proxy-manager"),
		containerProxyPath:                 containerProxyPath,
		containerProxyConfigPath:           containerProxyConfigPath,
		containerProxyTrustedCACerts:       ContainerProxyTrustedCACerts,
		containerProxyVerifySubjectAltName: ContainerProxyVerifySubjectAltName,
		containerProxyRequireClientCerts:   containerProxyRequireClientCerts,
		refreshDelayMS:                     refreshDelayMS,
	}
}

// This modifies the container pointer in order to create garden NetIn rules in the storenode.Create
func (p *ProxyConfigHandler) ProxyPorts(logger lager.Logger, container *executor.Container) ([]executor.ProxyPortMapping, []uint16) {
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

func (p *ProxyConfigHandler) CreateDir(logger lager.Logger, container executor.Container) ([]garden.BindMount, []executor.EnvironmentVariable, error) {
	if !container.EnableContainerProxy {
		return nil, nil, nil
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
		return nil, nil, err
	}

	return mounts, nil, nil
}

func (p *ProxyConfigHandler) RemoveDir(logger lager.Logger, container executor.Container) error {
	if !container.EnableContainerProxy {
		return nil
	}

	logger.Info("removing-container-proxy-config-dir")
	proxyConfigDir := filepath.Join(p.containerProxyConfigPath, container.Guid)
	return os.RemoveAll(proxyConfigDir)
}

func (p *ProxyConfigHandler) Update(credentials Credential, container executor.Container) error {
	if !container.EnableContainerProxy {
		return nil
	}

	return p.writeConfig(credentials, container)
}

func (p *ProxyConfigHandler) Close(invalidCredentials Credential, container executor.Container) error {
	if !container.EnableContainerProxy {
		return nil
	}

	err := p.writeConfig(invalidCredentials, container)
	if err != nil {
		return err
	}

	block, _ := pem.Decode([]byte(invalidCredentials.Cert))
	if block == nil {
		return ErrInvalidCertificate
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return err
	}
	return waitForEnvoyToReloadCerts(container, cert.SerialNumber)
}

func (p *ProxyConfigHandler) writeConfig(credentials Credential, container executor.Container) error {
	proxyConfigPath := filepath.Join(p.containerProxyConfigPath, container.Guid, "envoy.yaml")
	listenerConfigPath := filepath.Join(p.containerProxyConfigPath, container.Guid, "listeners.yaml")

	adminPort, err := getAvailablePort(container.Ports)
	if err != nil {
		return err
	}

	proxyConfig, err := generateProxyConfig(container, p.refreshDelayMS, adminPort)
	if err != nil {
		return err
	}

	err = writeProxyConfig(proxyConfig, proxyConfigPath)
	if err != nil {
		return err
	}

	listenerConfig, err := generateListenerConfig(
		container,
		credentials,
		p.containerProxyTrustedCACerts,
		p.containerProxyVerifySubjectAltName,
		p.containerProxyRequireClientCerts,
	)
	if err != nil {
		return err
	}

	err = writeListenerConfig(listenerConfig, listenerConfigPath)
	if err != nil {
		return err
	}

	return nil
}

func waitForEnvoyToReloadCerts(container executor.Container, newSerialNumber *big.Int) error {
	getSerialNumber := func() (*big.Int, error) {
		addr := fmt.Sprintf("%s:%d", container.InternalIP, container.Ports[0].ContainerTLSProxyPort)
		conn, err := tls.Dial("tcp", addr, &tls.Config{
			InsecureSkipVerify: true,
		})
		if err != nil {
			return nil, err
		}
		defer conn.Close()
		if err := conn.Handshake(); err != nil {
			return nil, err
		}

		return conn.ConnectionState().PeerCertificates[0].SerialNumber, nil
	}

	// TODO: test this
	for i := 0; i < 10; i++ {
		seriaNumber, err := getSerialNumber()
		if err != nil {
			return err
		}
		if err == nil && seriaNumber == newSerialNumber {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	return nil
}

func generateProxyConfig(container executor.Container, refreshDelayMS time.Duration, adminPort uint16) (envoy.ProxyConfig, error) {
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

func generateListenerConfig(container executor.Container, creds Credential, trustedCaCerts []string, subjectAltNames []string, requireClientCerts bool) (envoy.ListenerConfig, error) {
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
