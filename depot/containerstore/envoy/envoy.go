package envoy

type Config struct {
	StatPrefix string `yaml:"stat_prefix"` // envoy.tcp_proxy
	Cluster    string `yaml:"cluster"`
}

type Filter struct {
	Name   string `yaml:"name"`
	Config Config `yaml:"config"`
}

type DataSource struct {
	InlineString string `yaml:"inline_string"`
}

type TLSCertificate struct {
	CertificateChain DataSource `yaml:"certificate_chain"`
	PrivateKey       DataSource `yaml:"private_key"`
}

type CertificateValidationContext struct {
	TrustedCA            DataSource `yaml:"trusted_ca,omitempty"`
	VerifySubjectAltName []string   `yaml:"verify_subject_alt_name,omitempty"`
}

type SDSConfig struct {
	Path string `yaml:"path"`
}

type SecretConfig struct {
	Name      string    `yaml:"name"`
	SDSConfig SDSConfig `yaml:"sds_config"`
}

type CommonTLSContext struct {
	TLSCertificateSDSSecretConfigs   SecretConfig `yaml:"tls_certificate_sds_secret_configs"`
	ValidationContextSDSSecretConfig SecretConfig `yaml:"validation_context_sds_secret_config,omitempty"`
	TLSParams                        TLSParams    `yaml:"tls_params"`
}

type TLSParams struct {
	CipherSuites string `yaml:"cipher_suites"`
}

type TLSContext struct {
	CommonTLSContext         CommonTLSContext `yaml:"common_tls_context"`
	RequireClientCertificate bool             `yaml:"require_client_certificate"`
}

type FilterChain struct {
	Filters    []Filter   `yaml:"filters"`
	TLSContext TLSContext `yaml:"tls_context"`
}

type Listener struct {
	Name         string        `yaml:"name"`
	Address      Address       `yaml:"address"`
	FilterChains []FilterChain `yaml:"filter_chains"`
}

type SocketAddress struct {
	Address   string `yaml:"address"`
	PortValue uint16 `yaml:"port_value"`
}

type Address struct {
	SocketAddress SocketAddress `yaml:"socket_address"`
}

type Admin struct {
	AccessLogPath string  `yaml:"access_log_path"`
	Address       Address `yaml:"address"`
}

type Threshold struct {
	MaxConnections uint32 `yaml:"max_connections"`
}

type CircuitBreakers struct {
	Thresholds []Threshold `yamls:"thresholds"`
}

type Cluster struct {
	Name              string          `yaml:"name"`
	ConnectionTimeout string          `yaml:"connect_timeout"`
	Type              string          `yaml:"type"`
	LbPolicy          string          `yaml:"lb_policy"`
	Hosts             []Address       `yaml:"hosts"`
	CircuitBreakers   CircuitBreakers `yaml:"circuit_breakers"`
}

type StaticResources struct {
	Clusters  []Cluster  `yaml:"clusters"`
	Listeners []Listener `yaml:"listeners"`
}

type LDSConfig struct {
	Path string `yaml:"path"`
}

type DynamicResources struct {
	LDSConfig LDSConfig `yaml:"lds_config"`
}

type ProxyConfig struct {
	Admin           Admin           `yaml:"admin"`
	StaticResources StaticResources `yaml:"static_resources"`
}

type CAResource struct {
	Type              string                       `yaml:"@type"`
	Name              string                       `yaml:"name"`
	ValidationContext CertificateValidationContext `yaml:"validation_context"`
}

type SDSCAResource struct {
	VersionInfo string       `yaml:"version_info"`
	Resources   []CAResource `yaml:"resources"`
}

type CertificateResource struct {
	Type           string         `yaml:"@type"`
	Name           string         `yaml:"name"`
	TLSCertificate TLSCertificate `yaml:"tls_certificate"`
}

type SDSCertificateResource struct {
	VersionInfo string                `yaml:"version_info"`
	Resources   []CertificateResource `yaml:"resources"`
}
