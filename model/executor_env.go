package model // import "code.cloudfoundry.org/executor/model"

import (
	"sync"

	"code.cloudfoundry.org/durationjson"
	"code.cloudfoundry.org/lager"
	vcmodels "github.com/virtualcloudfoundry/vcontainercommon/vcontainermodels"
)

// global environment for the executor
// 1. indicates whether we need virtual store
// 2. provide container provider configs
type ExecutorConfig struct {
	AutoDiskOverheadMB                 int                   `json:"auto_disk_capacity_overhead_mb"`
	CachePath                          string                `json:"cache_path,omitempty"`
	ContainerInodeLimit                uint64                `json:"container_inode_limit,omitempty"`
	ContainerMaxCpuShares              uint64                `json:"container_max_cpu_shares,omitempty"`
	ContainerMetricsReportInterval     durationjson.Duration `json:"container_metrics_report_interval,omitempty"`
	ContainerOwnerName                 string                `json:"container_owner_name,omitempty"`
	ContainerReapInterval              durationjson.Duration `json:"container_reap_interval,omitempty"`
	CreateWorkPoolSize                 int                   `json:"create_work_pool_size,omitempty"`
	DeleteWorkPoolSize                 int                   `json:"delete_work_pool_size,omitempty"`
	DiskMB                             string                `json:"disk_mb,omitempty"`
	EnableDeclarativeHealthcheck       bool                  `json:"enable_declarative_healthcheck,omitempty"`
	DeclarativeHealthcheckPath         string                `json:"declarative_healthcheck_path,omitempty"`
	EnableContainerProxy               bool                  `json:"enable_container_proxy,omitempty"`
	EnvoyConfigRefreshDelay            durationjson.Duration `json:"envoy_config_refresh_delay"`
	EnvoyDrainTimeout                  durationjson.Duration `json:"envoy_drain_timeout"`
	ProxyMemoryAllocationMB            int                   `json:"proxy_memory_allocation_mb,omitempty"`
	ContainerProxyPath                 string                `json:"container_proxy_path,omitempty"`
	ContainerProxyConfigPath           string                `json:"container_proxy_config_path,omitempty"`
	ExportNetworkEnvVars               bool                  `json:"export_network_env_vars,omitempty"`
	GardenAddr                         string                `json:"garden_addr,omitempty"`
	GardenHealthcheckCommandRetryPause durationjson.Duration `json:"garden_healthcheck_command_retry_pause,omitempty"`
	GardenHealthcheckEmissionInterval  durationjson.Duration `json:"garden_healthcheck_emission_interval,omitempty"`
	GardenHealthcheckInterval          durationjson.Duration `json:"garden_healthcheck_interval,omitempty"`
	GardenHealthcheckProcessArgs       []string              `json:"garden_healthcheck_process_args,omitempty"`
	GardenHealthcheckProcessDir        string                `json:"garden_healthcheck_process_dir"`
	GardenHealthcheckProcessEnv        []string              `json:"garden_healthcheck_process_env,omitempty"`
	GardenHealthcheckProcessPath       string                `json:"garden_healthcheck_process_path"`
	GardenHealthcheckProcessUser       string                `json:"garden_healthcheck_process_user"`
	GardenHealthcheckTimeout           durationjson.Duration `json:"garden_healthcheck_timeout,omitempty"`
	GardenNetwork                      string                `json:"garden_network,omitempty"`
	GracefulShutdownInterval           durationjson.Duration `json:"graceful_shutdown_interval,omitempty"`
	HealthCheckContainerOwnerName      string                `json:"healthcheck_container_owner_name,omitempty"`
	HealthCheckWorkPoolSize            int                   `json:"healthcheck_work_pool_size,omitempty"`
	HealthyMonitoringInterval          durationjson.Duration `json:"healthy_monitoring_interval,omitempty"`
	InstanceIdentityCAPath             string                `json:"instance_identity_ca_path,omitempty"`
	InstanceIdentityCredDir            string                `json:"instance_identity_cred_dir,omitempty"`
	InstanceIdentityPrivateKeyPath     string                `json:"instance_identity_private_key_path,omitempty"`
	InstanceIdentityValidityPeriod     durationjson.Duration `json:"instance_identity_validity_period,omitempty"`
	MaxCacheSizeInBytes                uint64                `json:"max_cache_size_in_bytes,omitempty"`
	MaxConcurrentDownloads             int                   `json:"max_concurrent_downloads,omitempty"`
	MemoryMB                           string                `json:"memory_mb,omitempty"`
	MetricsWorkPoolSize                int                   `json:"metrics_work_pool_size,omitempty"`
	PathToCACertsForDownloads          string                `json:"path_to_ca_certs_for_downloads"`
	PathToTLSCert                      string                `json:"path_to_tls_cert"`
	PathToTLSKey                       string                `json:"path_to_tls_key"`
	PathToTLSCACert                    string                `json:"path_to_tls_ca_cert"`
	PostSetupHook                      string                `json:"post_setup_hook"`
	PostSetupUser                      string                `json:"post_setup_user"`
	ReadWorkPoolSize                   int                   `json:"read_work_pool_size,omitempty"`
	ReservedExpirationTime             durationjson.Duration `json:"reserved_expiration_time,omitempty"`
	SkipCertVerify                     bool                  `json:"skip_cert_verify,omitempty"`
	TempDir                            string                `json:"temp_dir,omitempty"`
	TrustedSystemCertificatesPath      string                `json:"trusted_system_certificates_path"`
	UnhealthyMonitoringInterval        durationjson.Duration `json:"unhealthy_monitoring_interval,omitempty"`
	VolmanDriverPaths                  string                `json:"volman_driver_paths"`
	CSIPaths                           []string              `json:"csi_paths"`
	CSIMountRootDir                    string                `json:"csi_mount_root_dir"`
}

func (config *ExecutorConfig) Validate(logger lager.Logger) bool {
	valid := true

	if config.ContainerMaxCpuShares == 0 {
		logger.Error("max-cpu-shares-invalid", nil)
		valid = false
	}

	if config.HealthyMonitoringInterval <= 0 {
		logger.Error("healthy-monitoring-interval-invalid", nil)
		valid = false
	}

	if config.UnhealthyMonitoringInterval <= 0 {
		logger.Error("unhealthy-monitoring-interval-invalid", nil)
		valid = false
	}

	if config.GardenHealthcheckInterval <= 0 {
		logger.Error("garden-healthcheck-interval-invalid", nil)
		valid = false
	}

	if config.GardenHealthcheckProcessUser == "" {
		logger.Error("garden-healthcheck-process-user-invalid", nil)
		valid = false
	}

	if config.GardenHealthcheckProcessPath == "" {
		logger.Error("garden-healthcheck-process-path-invalid", nil)
		valid = false
	}

	if config.PostSetupHook != "" && config.PostSetupUser == "" {
		logger.Error("post-setup-hook-requires-a-user", nil)
		valid = false
	}

	return valid
}

type ExecutorEnv struct {
	Config                 ExecutorConfig
	VContainerClientConfig vcmodels.VContainerClientConfig
}

var instance *ExecutorEnv
var once sync.Once

func GetExecutorEnvInstance() *ExecutorEnv {
	once.Do(func() {
		instance = &ExecutorEnv{}
	})
	return instance
}
