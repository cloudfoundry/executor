package model // import "code.cloudfoundry.org/executor/model"

type ContainerProviderConfig struct {
	Location        string `json:"location"`
	ContainerId     string `json:"container_id"`
	ContainerSecret string `json:"container_secret"`
	SubscriptionId  string `json:"subscription_id"`
	OptionalParam1  string `json:"optional_param_1,omitempty"`
	StorageId       string `json:"storage_id"`
	StorageSecret   string `json:"storage_secret"`
}
