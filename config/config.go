package config

import (
	"encoding/json"
	"os"

	"code.cloudfoundry.org/lager/lagerflags"
)

// TODO encrypt the configs
type ACIConfig struct {
	Location        string `json:"location"`
	ResourceGroup   string `json:"resource_group"`
	ContainerId     string `json:"container_id"`
	ContainerSecret string `json:"container_secret"`
	SubscriptionId  string `json:"subscription_id"`
	OptionalParam1  string `json:"optional_param_1,omitempty"`
	StorageId       string `json:"storage_id"`
	StorageSecret   string `json:"storage_secret"`
}

type SMBProxy struct {
	IP   string
	Port int
}

type VContainerConfig struct {
	CaFile                          string `json:"ca_file"`
	CellID                          string `json:"cell_id"`
	CertFile                        string `json:"cert_file"`
	ConsulCluster                   string `json:"consul_cluster,omitempty"`
	EnableConsulServiceRegistration bool   `json:"enable_consul_service_registration"`
	KeyFile                         string `json:"key_file"`
	ListenAddress                   string `json:"listen_address"`
	SessionName                     string `json:"session_name,omitempty"`

	ContainerServiceProvider string    `json:"container_service_provider,omitempty"`
	ACIConfig                ACIConfig `json:"azure_container_provider_cfg"`
	SMBProxy                 SMBProxy  `json:"smb_proxy,omitempty"`
	lagerflags.LagerConfig
}

func defaultConfig() VContainerConfig {
	return VContainerConfig{
		LagerConfig: lagerflags.DefaultLagerConfig(),
		SessionName: "vcontainer",
	}
}

func NewVContainerConfig(configPath string) (VContainerConfig, error) {
	repConfig := defaultConfig()
	configFile, err := os.Open(configPath)
	if err != nil {
		return VContainerConfig{}, err
	}

	defer configFile.Close()

	decoder := json.NewDecoder(configFile)

	err = decoder.Decode(&repConfig)
	if err != nil {
		return VContainerConfig{}, err
	}

	return repConfig, nil
}
