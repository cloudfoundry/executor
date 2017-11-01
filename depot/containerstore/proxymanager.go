package containerstore

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"time"

	uuid "github.com/nu7hatch/gouuid"
	"github.com/tedsuo/ifrit"

	"code.cloudfoundry.org/executor"
	"code.cloudfoundry.org/lager"
)

type Route struct {
	Cluster string `json:"cluster"`
}

type RouteConfig struct {
	Routes []Route `json:"routes"`
}

type Config struct {
	StatPrefix  string      `json:"stat_prefix"`
	RouteConfig RouteConfig `json:"route_config"`
}

type Filter struct {
	Type   string `json:"type"`
	Name   string `json:"name"`
	Config Config `json:"config"`
}

type SSLContext struct {
	CertChainFile  string `json:"cert_chain_file"`
	PrivateKeyFile string `json:"private_key_file"`
}

type Listener struct {
	Address    string     `json:"address"`
	Filters    []Filter   `json:"filters"`
	SSLContext SSLContext `json:"ssl_context"`
}

type Admin struct {
	AccessLogPath string `json:"access_log_path"`
	Address       string `json:"address"`
}

type Host struct {
	URL string `json:"url"`
}

type Cluster struct {
	Name                string `json:"name"`
	ConnectionTimeoutMs int    `json:"connect_timeout_ms"`
	Type                string `json:"type"`
	LbType              string `json:"lb_type"`
	Hosts               []Host `json:"hosts"`
}

type ClusterManager struct {
	Clusters []Cluster `json:"clusters"`
}

type LDS struct {
	Cluster        string `json:"cluster"`
	RefreshDelayMS string `json:"refresh_delay_ms"`
}

type ProxyConfig struct {
	Listeners      []Listener     `json:"listeners"`
	Admin          Admin          `json:"admin"`
	ClusterManager ClusterManager `json:"cluster_manager"`
	LDS            LDS            `json:"lds"`
}

type ListenerConfig struct {
	Listeners []Listener `json:"listeners"`
}

const (
	TimeOut    = 250
	Static     = "static"
	RoundRobin = "round_robin"

	Read       = "read"
	IngressTCP = "ingress_tcp"
	TcpProxy   = "tcp_proxy"

	AdminAccessLog = "/dev/null"
)

//go:generate counterfeiter -o containerstorefakes/fake_proxymanager.go . ProxyManager
type ProxyManager interface {
	Runner(lager.Logger, executor.Container, <-chan struct{}) ifrit.Runner
}

type proxyManager struct {
	logger                   lager.Logger
	containerProxyConfigPath string
	refreshDelayMS           time.Duration
}

func NewProxyManager(
	logger lager.Logger,
	containerProxyConfigPath string,
	refreshDelayMS time.Duration,
) ProxyManager {
	return &proxyManager{
		logger: logger.Session("proxy-manager"),
		containerProxyConfigPath: containerProxyConfigPath,
		refreshDelayMS:           refreshDelayMS,
	}
}

func (p *proxyManager) Runner(logger lager.Logger, container executor.Container, credRotatedChan <-chan struct{}) ifrit.Runner {
	return ifrit.RunFunc(func(signals <-chan os.Signal, ready chan<- struct{}) error {
		logger = logger.Session("proxy-manager")
		logger.Info("starting")
		defer logger.Info("finished")

		proxyConfigPath := filepath.Join(p.containerProxyConfigPath, container.Guid, "envoy.json")
		listenerConfigPath := filepath.Join(p.containerProxyConfigPath, container.Guid, "listeners.json")

		proxyConfig := generateProxyConfig(logger, container, p.refreshDelayMS)

		err := writeProxyConfig(proxyConfig, proxyConfigPath)
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
	})
}

func generateProxyConfig(logger lager.Logger, container executor.Container, refreshDelayMS time.Duration) ProxyConfig {
	clusters := []Cluster{}
	for index, portMap := range container.Ports {
		clusterName := fmt.Sprintf("%d-service-cluster", index)
		clusterAddress := fmt.Sprintf("tcp://127.0.0.1:%d", portMap.ContainerPort)
		clusters = append(clusters, Cluster{
			Name:                clusterName,
			ConnectionTimeoutMs: TimeOut,
			Type:                Static,
			LbType:              RoundRobin,
			Hosts:               []Host{Host{URL: clusterAddress}},
		})
	}

	clusters = append(clusters, Cluster{
		Name:                "lds-cluster",
		ConnectionTimeoutMs: TimeOut,
		Type:                Static,
		LbType:              RoundRobin,
		Hosts:               []Host{Host{URL: "tcp://127.0.0.1:9933"}},
	})

	config := ProxyConfig{
		Admin: Admin{
			AccessLogPath: AdminAccessLog,
			Address:       "tcp://127.0.0.1:9901",
		},
		Listeners: []Listener{},
		ClusterManager: ClusterManager{
			Clusters: clusters,
		},
		LDS: LDS{
			Cluster:        "lds-cluster",
			RefreshDelayMS: strconv.Itoa(int(refreshDelayMS.Seconds() * 1000)),
		},
	}
	return config
}

func writeProxyConfig(proxyConfig ProxyConfig, path string) error {
	data, err := json.Marshal(proxyConfig)
	if err != nil {
		return err
	}

	return ioutil.WriteFile(path, data, 0666)
}

func writeListenerConfig(listenerConfig ListenerConfig, path string) error {
	data, err := json.Marshal(listenerConfig)
	if err != nil {
		return err
	}

	return ioutil.WriteFile(path, data, 0666)
}

func generateListenerConfig(logger lager.Logger, container executor.Container) (ListenerConfig, error) {
	listeners := []Listener{}

	for index, portMap := range container.Ports {
		listenerName := TcpProxy
		listenerAddress := fmt.Sprintf("tcp://0.0.0.0:%d", portMap.ContainerTLSProxyPort)
		containerMountPath := "/etc/cf-instance-credentials"
		clusterName := fmt.Sprintf("%d-service-cluster", index)

		newUUID, err := uuid.NewV4()
		if err != nil {
			logger.Error("failed-to-create-uuid-for-stat-prefix", err)
			return ListenerConfig{}, err
		}

		listeners = append(listeners, Listener{
			Address: listenerAddress,
			Filters: []Filter{Filter{
				Type: Read,
				Name: listenerName,
				Config: Config{
					StatPrefix: IngressTCP + "-" + newUUID.String(),
					RouteConfig: RouteConfig{
						Routes: []Route{Route{Cluster: clusterName}},
					},
				},
			},
			},
			SSLContext: SSLContext{
				CertChainFile:  path.Join(containerMountPath, "instance.crt"),
				PrivateKeyFile: path.Join(containerMountPath, "instance.key"),
			},
		})
	}

	config := ListenerConfig{
		Listeners: listeners,
	}

	return config, nil
}
