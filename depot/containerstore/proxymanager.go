package containerstore

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"time"

	uuid "github.com/nu7hatch/gouuid"
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

var dummyRunner = func(credRotatedChan <-chan struct{}) ifrit.Runner {
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
	Runner(lager.Logger, executor.Container, <-chan struct{}) (ProxyRunner, error)
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
	logger                   lager.Logger
	containerProxyPath       string
	containerProxyConfigPath string
	refreshDelayMS           time.Duration
}

type noopProxyManager struct{}

func (p *noopProxyManager) BindMounts(logger lager.Logger, container executor.Container) ([]garden.BindMount, error) {
	return []garden.BindMount{}, nil
}

func (p *noopProxyManager) ProxyPorts(lager.Logger, *executor.Container) ([]executor.ProxyPortMapping, []uint16) {
	return nil, nil
}

func (p *noopProxyManager) Runner(logger lager.Logger, container executor.Container, credRotatedChan <-chan struct{}) (ProxyRunner, error) {
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
	refreshDelayMS time.Duration,
) ProxyManager {
	return &proxyManager{
		logger:                   logger.Session("proxy-manager"),
		containerProxyPath:       containerProxyPath,
		containerProxyConfigPath: containerProxyConfigPath,
		refreshDelayMS:           refreshDelayMS,
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

func (p *proxyManager) Runner(logger lager.Logger, container executor.Container, credRotatedChan <-chan struct{}) (ProxyRunner, error) {
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

			listenerConfig, err := generateListenerConfig(logger, container)
			if err != nil {
				logger.Error("failed-generating-initial-proxy-listener-config", err)
				return err
			}

			err = writeListenerConfig(listenerConfig, listenerConfigPath)
			if err != nil {
				logger.Error("failed-writing-initial-proxy-listener-config", err)
				return err
			}

			close(ready)
			logger.Info("started")

			for {
				select {
				case <-credRotatedChan:
					logger = logger.Session("updating-proxy-listener-config")
					logger.Debug("started")

					listenerConfig, err := generateListenerConfig(logger, container)
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
				case signal := <-signals:
					logger.Info("signaled", lager.Data{"signal": signal.String()})
					configPath := filepath.Join(p.containerProxyConfigPath, container.Guid)
					p.logger.Info("cleanup-proxy-config-path", lager.Data{"config-path": configPath})
					return os.RemoveAll(configPath)
				}
			}
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
			Hosts:             []envoy.Address{envoy.Address{SocketAddress: envoy.SocketAddress{Address: "127.0.0.1", PortValue: portMap.ContainerPort}}},
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

func generateListenerConfig(logger lager.Logger, container executor.Container) (envoy.ListenerConfig, error) {
	resources := []envoy.Resource{}

	for index, portMap := range container.Ports {
		listenerName := TcpProxy
		containerMountPath := "/etc/cf-instance-credentials"
		clusterName := fmt.Sprintf("%d-service-cluster", index)

		newUUID, err := uuid.NewV4()
		if err != nil {
			logger.Error("failed-to-create-uuid-for-stat-prefix", err)
			return envoy.ListenerConfig{}, err
		}

		resources = append(resources, envoy.Resource{
			Type:    "type.googleapis.com/envoy.api.v2.Listener",
			Name:    fmt.Sprintf("listener-%d", portMap.ContainerPort),
			Address: envoy.Address{SocketAddress: envoy.SocketAddress{Address: "0.0.0.0", PortValue: portMap.ContainerTLSProxyPort}},
			FilterChains: []envoy.FilterChain{envoy.FilterChain{
				Filters: []envoy.Filter{
					envoy.Filter{
						Name: listenerName,
						Config: envoy.Config{
							StatPrefix: IngressListener + "_" + newUUID.String(),
							Cluster:    clusterName,
						},
					},
				},
				TLSContext: envoy.TLSContext{
					CommonTLSContext: envoy.CommonTLSContext{
						TLSParams: envoy.TLSParams{
							CipherSuites: SupportedCipherSuites,
						},
						TLSCertificates: []envoy.TLSCertificate{
							envoy.TLSCertificate{
								CertificateChain: envoy.File{Filename: path.Join(containerMountPath, "instance.crt")},
								PrivateKey:       envoy.File{Filename: path.Join(containerMountPath, "instance.key")},
							},
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
