package services_bbs

import (
	steno "github.com/cloudfoundry/gosteno"
	"github.com/cloudfoundry/storeadapter"
)

type ServicesBBS struct {
	store  storeadapter.StoreAdapter
	logger *steno.Logger
}

func New(store storeadapter.StoreAdapter, logger *steno.Logger) *ServicesBBS {
	return &ServicesBBS{
		store:  store,
		logger: logger,
	}
}
