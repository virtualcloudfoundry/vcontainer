package vcontainer

import (
	"github.com/Azure/go-autorest/autorest/azure"
	"github.com/virtualcloudfoundry/goaci"
	"github.com/virtualcloudfoundry/goaci/aci"
	"github.com/virtualcloudfoundry/vcontainer/config"
)

func NewACIClient() (*aci.Client, error) {
	var azAuth *goaci.Authentication
	config := config.GetVContainerEnvInstance().ACIConfig
	azAuth = goaci.NewAuthentication(azure.PublicCloud.Name, config.ContainerId, config.ContainerSecret, config.SubscriptionId, config.OptionalParam1)

	aciClient, err := aci.NewClient(azAuth)
	return aciClient, err
}
