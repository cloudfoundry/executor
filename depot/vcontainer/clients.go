package vcontainer

import (
	"code.cloudfoundry.org/lager"
	"github.com/virtualcloudfoundry/vcontainerclient"
	"github.com/virtualcloudfoundry/vcontainercommon/vcontainermodels"
)

func NewVContainerClient(logger lager.Logger, config vcontainermodels.VContainerClientConfig) (vcontainermodels.VContainerClient, error) {
	vcontainerClient, err := vcontainerclient.NewVContainerClient(logger, config)
	return vcontainerClient, err
}

func NewVGardenClient(logger lager.Logger, config vcontainermodels.VContainerClientConfig) (vcontainermodels.VGardenClient, error) {
	vgardenClient, err := vcontainerclient.NewVGardenClient(logger, config)
	return vgardenClient, err
}

func NewVProcessClient(logger lager.Logger, config vcontainermodels.VContainerClientConfig) (vcontainermodels.VProcessClient, error) {
	vgardenClient, err := vcontainerclient.NewVProcessClient(logger, config)
	return vgardenClient, err
}
