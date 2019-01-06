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
	"strconv"
	"strings"
	"time"

	"code.cloudfoundry.org/clock"
	"code.cloudfoundry.org/executor"
	"code.cloudfoundry.org/executor/depot/containerstore/envoy"
	"code.cloudfoundry.org/garden"
	"code.cloudfoundry.org/lager"
	ghodss_yaml "github.com/ghodss/yaml"
	"github.com/tedsuo/ifrit"
	yaml "gopkg.in/yaml.v2"

	envoy_v2 "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	envoy_v2_auth "github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	envoy_v2_cluster "github.com/envoyproxy/go-control-plane/envoy/api/v2/cluster"
	envoy_v2_core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	envoy_v2_listener "github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	envoy_v2_bootstrap "github.com/envoyproxy/go-control-plane/envoy/config/bootstrap/v2"
	envoy_v2_tcp_proxy_filter "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/tcp_proxy/v2"
	envoy_util "github.com/envoyproxy/go-control-plane/pkg/util"
	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/proto"
	proto_types "github.com/gogo/protobuf/types"
)

const (
	StartProxyPort = 61001
	EndProxyPort   = 65534

	TimeOut = 250 * time.Millisecond

	IngressListener = "ingress_listener"
	TcpProxy        = "envoy.tcp_proxy"

	AdminAccessLog = os.DevNull
)

var (
	ErrNoPortsAvailable   = errors.New("no ports available")
	ErrInvalidCertificate = errors.New("cannot parse invalid certificate")

	SupportedCipherSuites = []string{"ECDHE-RSA-AES256-GCM-SHA384", "ECDHE-RSA-AES128-GCM-SHA256"}
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

	reloadDuration time.Duration
	reloadClock    clock.Clock

	adsServers []string
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
	reloadDuration time.Duration,
	reloadClock clock.Clock,
	adsServers []string,
) *ProxyConfigHandler {
	return &ProxyConfigHandler{
		logger:                             logger.Session("proxy-manager"),
		containerProxyPath:                 containerProxyPath,
		containerProxyConfigPath:           containerProxyConfigPath,
		containerProxyTrustedCACerts:       ContainerProxyTrustedCACerts,
		containerProxyVerifySubjectAltName: ContainerProxyVerifySubjectAltName,
		containerProxyRequireClientCerts:   containerProxyRequireClientCerts,
		reloadDuration:                     reloadDuration,
		reloadClock:                        reloadClock,
		adsServers:                         adsServers,
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

	p.reloadClock.Sleep(p.reloadDuration)
	return nil
}

func (p *ProxyConfigHandler) writeConfig(credentials Credential, container executor.Container) error {
	proxyConfigPath := filepath.Join(p.containerProxyConfigPath, container.Guid, "envoy.yaml")
	sdsServerCertAndKeyPath := filepath.Join(p.containerProxyConfigPath, container.Guid, "sds-server-cert-and-key.yaml")
	sdsServerValidationContextPath := filepath.Join(p.containerProxyConfigPath, container.Guid, "sds-server-validation-context.yaml")

	adminPort, err := getAvailablePort(container.Ports)
	if err != nil {
		return err
	}

	proxyConfig, err := generateProxyConfig(
		container,
		adminPort,
		p.containerProxyRequireClientCerts,
		p.adsServers,
	)
	if err != nil {
		return err
	}

	err = writeProxyConfig(proxyConfig, proxyConfigPath)
	if err != nil {
		return err
	}

	sdsServerCertAndKey := generateSDSCertAndKey(container, credentials)
	err = writeDiscoveryResponseYAML(sdsServerCertAndKey, sdsServerCertAndKeyPath)
	if err != nil {
		return err
	}

	sdsServerValidationContext, err := generateSDSCAResource(
		container,
		credentials,
		p.containerProxyTrustedCACerts,
		p.containerProxyVerifySubjectAltName,
	)
	if err != nil {
		return err
	}
	err = marshalAndWriteToFile(sdsServerValidationContext, sdsServerValidationContextPath)
	if err != nil {
		return err
	}

	return nil
}

func envoyAddr(ip string, port uint16) *envoy_v2_core.Address {
	return &envoy_v2_core.Address{
		Address: &envoy_v2_core.Address_SocketAddress{
			SocketAddress: &envoy_v2_core.SocketAddress{
				Address: ip,
				PortSpecifier: &envoy_v2_core.SocketAddress_PortValue{
					PortValue: uint32(port),
				},
			},
		},
	}
}

func generateProxyConfig(
	container executor.Container,
	adminPort uint16,
	requireClientCerts bool,
	adsServers []string,
) (*envoy_v2_bootstrap.Bootstrap, error) {
	clusters := []envoy_v2.Cluster{}
	for index, portMap := range container.Ports {
		clusterName := fmt.Sprintf("%d-service-cluster", index)
		clusters = append(clusters, envoy_v2.Cluster{
			Name:           clusterName,
			ConnectTimeout: TimeOut,
			Hosts: []*envoy_v2_core.Address{
				envoyAddr(container.InternalIP, portMap.ContainerPort),
			},
			CircuitBreakers: &envoy_v2_cluster.CircuitBreakers{
				Thresholds: []*envoy_v2_cluster.CircuitBreakers_Thresholds{
					{MaxConnections: &proto_types.UInt32Value{Value: math.MaxUint32}},
				}},
		})
	}

	listeners, err := generateListeners(container, requireClientCerts)
	if err != nil {
		return nil, fmt.Errorf("generating listeners: %s", err)
	}

	config := &envoy_v2_bootstrap.Bootstrap{
		Admin: &envoy_v2_bootstrap.Admin{
			AccessLogPath: AdminAccessLog,
			Address:       envoyAddr("127.0.0.1", adminPort),
		},
		StaticResources: &envoy_v2_bootstrap.Bootstrap_StaticResources{
			Listeners: listeners,
		},
	}

	if len(adsServers) > 0 {
		adsHosts := []*envoy_v2_core.Address{}
		for _, a := range adsServers {
			address, port, err := splitHost(a)
			if err != nil {
				return nil, err
			}
			adsHosts = append(adsHosts, envoyAddr(address, port))
		}

		clusters = append(clusters, envoy_v2.Cluster{
			Name:                 "pilot-ads",
			ConnectTimeout:       TimeOut,
			Hosts:                adsHosts,
			Http2ProtocolOptions: &envoy_v2_core.Http2ProtocolOptions{},
		})

		dynamicResources := &envoy_v2_bootstrap.Bootstrap_DynamicResources{
			LdsConfig: adsConfigSource,
			CdsConfig: adsConfigSource,
			AdsConfig: &envoy_v2_core.ApiConfigSource{
				ApiType: envoy_v2_core.ApiConfigSource_GRPC,
				GrpcServices: []*envoy_v2_core.GrpcService{
					{
						TargetSpecifier: &envoy_v2_core.GrpcService_EnvoyGrpc_{
							EnvoyGrpc: &envoy_v2_core.GrpcService_EnvoyGrpc{
								ClusterName: "pilot-ads",
							},
						},
					},
				},
			},
		}
		config.DynamicResources = dynamicResources
	}

	config.StaticResources.Clusters = clusters

	return config, nil
}

var adsConfigSource = &envoy_v2_core.ConfigSource{
	ConfigSourceSpecifier: &envoy_v2_core.ConfigSource_Ads{
		Ads: &envoy_v2_core.AggregatedConfigSource{},
	},
}

func splitHost(host string) (string, uint16, error) {
	parts := strings.Split(host, ":")
	if len(parts) != 2 {
		return "", 0, fmt.Errorf("ads server address is invalid: %s", host)
	}

	port, err := strconv.Atoi(parts[1])
	if err != nil {
		return "", 0, fmt.Errorf("ads server address is invalid: %s", host)
	}
	return parts[0], uint16(port), nil
}

func writeProxyConfig(proxyConfig *envoy_v2_bootstrap.Bootstrap, path string) error {
	jsonMarshaler := jsonpb.Marshaler{OrigName: true, EmitDefaults: false}
	jsonStr, err := jsonMarshaler.MarshalToString(proxyConfig)
	if err != nil {
		return err
	}
	yamlStr, err := ghodss_yaml.JSONToYAML([]byte(jsonStr))
	if err != nil {
		return err
	}
	return ioutil.WriteFile(path, yamlStr, 0666)
}

func marshalAndWriteToFile(toMarshal interface{}, path string) error {
	tmpPath := path + ".tmp"

	data, err := yaml.Marshal(toMarshal)
	if err != nil {
		return err
	}

	err = ioutil.WriteFile(tmpPath, data, 0666)
	if err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func generateListeners(container executor.Container, requireClientCerts bool) ([]envoy_v2.Listener, error) {
	listeners := []envoy_v2.Listener{}

	for index, portMap := range container.Ports {
		listenerName := TcpProxy
		clusterName := fmt.Sprintf("%d-service-cluster", index)

		filterConfig, err := envoy_util.MessageToStruct(&envoy_v2_tcp_proxy_filter.TcpProxy{
			StatPrefix: fmt.Sprintf("%d-stats", index),
			ClusterSpecifier: &envoy_v2_tcp_proxy_filter.TcpProxy_Cluster{
				Cluster: clusterName,
			},
		})

		if err != nil {
			return nil, err
		}

		listener := envoy_v2.Listener{
			Name:    fmt.Sprintf("listener-%d", portMap.ContainerPort),
			Address: *envoyAddr("0.0.0.0", portMap.ContainerTLSProxyPort),
			FilterChains: []envoy_v2_listener.FilterChain{
				{
					Filters: []envoy_v2_listener.Filter{
						{
							Name: listenerName,
							ConfigType: &envoy_v2_listener.Filter_Config{
								Config: filterConfig,
							},
						},
					},
					TlsContext: &envoy_v2_auth.DownstreamTlsContext{
						RequireClientCertificate: &proto_types.BoolValue{Value: requireClientCerts},
						CommonTlsContext: &envoy_v2_auth.CommonTlsContext{
							TlsCertificateSdsSecretConfigs: []*envoy_v2_auth.SdsSecretConfig{
								{
									Name: "server-cert-and-key",
									SdsConfig: &envoy_v2_core.ConfigSource{
										ConfigSourceSpecifier: &envoy_v2_core.ConfigSource_Path{
											Path: "/etc/cf-assets/envoy_config/sds-server-cert-and-key.yaml",
										},
									},
								},
							},
							TlsParams: &envoy_v2_auth.TlsParameters{
								CipherSuites: SupportedCipherSuites,
							},
						},
					},
				},
			},
		}

		if requireClientCerts {
			listener.FilterChains[0].TlsContext.CommonTlsContext.ValidationContextType = &envoy_v2_auth.CommonTlsContext_ValidationContextSdsSecretConfig{
				ValidationContextSdsSecretConfig: &envoy_v2_auth.SdsSecretConfig{
					Name: "server-validation-context",
					SdsConfig: &envoy_v2_core.ConfigSource{
						ConfigSourceSpecifier: &envoy_v2_core.ConfigSource_Path{
							Path: "/etc/cf-assets/envoy_config/sds-server-validation-context.yaml",
						},
					},
				},
			}
		}

		listeners = append(listeners, listener)
	}

	return listeners, nil
}

func generateSDSCertAndKey(container executor.Container, creds Credential) proto.Message {
	return &envoy_v2_auth.Secret{
		Name: "server-cert-and-key",
		Type: &envoy_v2_auth.Secret_TlsCertificate{
			TlsCertificate: &envoy_v2_auth.TlsCertificate{
				CertificateChain: &envoy_v2_core.DataSource{
					Specifier: &envoy_v2_core.DataSource_InlineString{
						InlineString: creds.Cert,
					},
				},
				PrivateKey: &envoy_v2_core.DataSource{
					Specifier: &envoy_v2_core.DataSource_InlineString{
						InlineString: creds.Key,
					},
				},
			},
		},
	}
}

func generateSDSCAResource(container executor.Container, creds Credential, trustedCaCerts []string, subjectAltNames []string) (envoy.SDSCAResource, error) {
	certs, err := pemConcatenate(trustedCaCerts)
	if err != nil {
		return envoy.SDSCAResource{}, err
	}

	resources := []envoy.CAResource{
		{
			Type: "type.googleapis.com/envoy.api.v2.auth.Secret",
			Name: "server-validation-context",
			ValidationContext: envoy.CertificateValidationContext{
				TrustedCA:            envoy.DataSource{InlineString: certs},
				VerifySubjectAltName: subjectAltNames,
			},
		},
	}

	return envoy.SDSCAResource{VersionInfo: "0", Resources: resources}, nil
}

func writeDiscoveryResponseYAML(resourceMsg proto.Message, outPath string) error {
	resourceAny, err := proto_types.MarshalAny(resourceMsg)
	if err != nil {
		return err
	}
	dr := &envoy_v2.DiscoveryResponse{
		VersionInfo: "0",
		Resources: []proto_types.Any{
			*resourceAny,
		},
	}
	jsonMarshaler := jsonpb.Marshaler{OrigName: true, EmitDefaults: false}
	fullJSON, err := jsonMarshaler.MarshalToString(dr)
	if err != nil {
		return err
	}

	yamlStr, err := ghodss_yaml.JSONToYAML([]byte(fullJSON))
	if err != nil {
		return err
	}
	return ioutil.WriteFile(outPath, yamlStr, 0666)
}

func pemConcatenate(certs []string) (string, error) {
	var certificateBuf bytes.Buffer
	for _, cert := range certs {
		rest := []byte(cert)
		totalCertLen := len(rest)
		var block *pem.Block
		for {
			block, rest = pem.Decode(rest)
			if block == nil {
				if len(rest) == totalCertLen {
					return "", errors.New("failed to read certificate")
				}
				break
			}
			err := pem.Encode(&certificateBuf, block)
			if err != nil {
				return "", err
			}
		}
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
