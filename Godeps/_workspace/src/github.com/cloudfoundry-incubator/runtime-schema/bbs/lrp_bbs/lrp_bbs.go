package lrp_bbs

import "github.com/cloudfoundry/storeadapter"

type LongRunningProcessBBS struct {
	store storeadapter.StoreAdapter
}

func New(store storeadapter.StoreAdapter) *LongRunningProcessBBS {
	return &LongRunningProcessBBS{
		store: store,
	}
}
