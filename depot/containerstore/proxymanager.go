package containerstore

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path"

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

type ProxyConfig struct {
	Listeners      []Listener     `json:"listeners"`
	Admin          Admin          `json:"admin"`
	ClusterManager ClusterManager `json:"cluster_manager"`
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

type ProxyManager interface {
	Run(signals <-chan os.Signal, ready chan<- struct{}) error
}

type proxyManager struct {
	logger                   lager.Logger
	containerProxyConfigPath string
}

func NewProxyManager(
	logger lager.Logger,
	containerProxyConfigPath string,
) ProxyManager {
	return &proxyManager{
		logger: logger.Session("proxy-manager"),
		containerProxyConfigPath: containerProxyConfigPath,
	}
}

func (p *proxyManager) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	p.logger.Info("started")
	close(ready)
	for {
		select {
		case signal := <-signals:
			p.logger.Info("signalled", lager.Data{"signal": signal.String()})
			return p.removeEnvoyConfigs()
		}
	}
}

func (p *proxyManager) removeEnvoyConfigs() error {
	p.logger.Info("cleanup-proxy-config-path", lager.Data{"config-path": p.containerProxyConfigPath})
	return os.RemoveAll(p.containerProxyConfigPath)
}

func GenerateProxyConfig(logger lager.Logger, portMapping []executor.ProxyPortMapping) ProxyConfig {
	listeners := []Listener{}
	clusters := []Cluster{}
	for index, portMap := range portMapping {
		clusterName := fmt.Sprintf("%d-service-cluster", index)
		listenerName := TcpProxy
		listenerAddress := fmt.Sprintf("tcp://0.0.0.0:%d", portMap.ProxyPort)
		containerMountPath := "/etc/cf-instance-credentials"
		clusterAddress := fmt.Sprintf("tcp://127.0.0.1:%d", portMap.AppPort)
		clusters = append(clusters, Cluster{
			Name:                clusterName,
			ConnectionTimeoutMs: TimeOut,
			Type:                Static,
			LbType:              RoundRobin,
			Hosts:               []Host{Host{URL: clusterAddress}},
		})

		listeners = append(listeners, Listener{
			Address: listenerAddress,
			Filters: []Filter{Filter{
				Type: Read,
				Name: listenerName,
				Config: Config{
					StatPrefix: IngressTCP,
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

	config := ProxyConfig{
		Admin: Admin{
			AccessLogPath: AdminAccessLog,
			Address:       "tcp://127.0.0.1:9901",
		},
		Listeners: listeners,
		ClusterManager: ClusterManager{
			Clusters: clusters,
		},
	}
	return config
}

func WriteProxyConfig(proxyConfig ProxyConfig, path string) error {
	data, err := json.Marshal(proxyConfig)
	if err != nil {
		return err
	}

	return ioutil.WriteFile(path, data, 0666)
}
