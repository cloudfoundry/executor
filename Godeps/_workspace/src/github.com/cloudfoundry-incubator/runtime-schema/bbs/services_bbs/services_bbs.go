package services_bbs

import "github.com/cloudfoundry/storeadapter"

type ServicesBBS struct {
	store storeadapter.StoreAdapter
}

func New(store storeadapter.StoreAdapter) *ServicesBBS {
	return &ServicesBBS{
		store: store,
	}
}
