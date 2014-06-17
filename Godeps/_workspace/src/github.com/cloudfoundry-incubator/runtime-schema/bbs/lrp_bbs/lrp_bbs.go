package lrp_bbs

import (
	"github.com/cloudfoundry-incubator/runtime-schema/bbs/shared"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/gosteno"
	"github.com/cloudfoundry/gunk/timeprovider"
	"github.com/cloudfoundry/storeadapter"
)

type LRPBBS struct {
	store        storeadapter.StoreAdapter
	timeProvider timeprovider.TimeProvider
	logger       *gosteno.Logger
}

func New(store storeadapter.StoreAdapter, timeProvider timeprovider.TimeProvider, logger *gosteno.Logger) *LRPBBS {
	return &LRPBBS{
		store:        store,
		timeProvider: timeProvider,
		logger:       logger,
	}
}

func (bbs *LRPBBS) DesireLRP(lrp models.DesiredLRP) error {
	return shared.RetryIndefinitelyOnStoreTimeout(func() error {
		return bbs.store.SetMulti([]storeadapter.StoreNode{
			{
				Key:   shared.DesiredLRPSchemaPath(lrp),
				Value: lrp.ToJSON(),
			},
		})
	})
}

func (bbs *LRPBBS) RemoveDesiredLRPByProcessGuid(processGuid string) error {
	return shared.RetryIndefinitelyOnStoreTimeout(func() error {
		err := bbs.store.Delete(shared.DesiredLRPSchemaPathByProcessGuid(processGuid))
		if err == storeadapter.ErrorKeyNotFound {
			return nil
		}
		return err
	})
	return nil
}

func (bbs *LRPBBS) ChangeDesiredLRP(change models.DesiredLRPChange) error {
	return shared.RetryIndefinitelyOnStoreTimeout(func() error {
		if change.Before != nil && change.After != nil {
			return bbs.store.CompareAndSwap(
				storeadapter.StoreNode{
					Key:   shared.DesiredLRPSchemaPath(*change.Before),
					Value: (*change.Before).ToJSON(),
				},
				storeadapter.StoreNode{
					Key:   shared.DesiredLRPSchemaPath(*change.After),
					Value: (*change.After).ToJSON(),
				},
			)
		}

		if change.Before != nil {
			return bbs.store.CompareAndDelete(
				storeadapter.StoreNode{
					Key:   shared.DesiredLRPSchemaPath(*change.Before),
					Value: (*change.Before).ToJSON(),
				},
			)
		}

		if change.After != nil {
			return bbs.store.Create(
				storeadapter.StoreNode{
					Key:   shared.DesiredLRPSchemaPath(*change.After),
					Value: (*change.After).ToJSON(),
				},
			)
		}

		return nil
	})
}

func (bbs *LRPBBS) RemoveActualLRP(lrp models.ActualLRP) error {
	return shared.RetryIndefinitelyOnStoreTimeout(func() error {
		return bbs.store.Delete(shared.ActualLRPSchemaPath(lrp))
	})
}

func (bbs *LRPBBS) ReportActualLRPAsStarting(lrp models.ActualLRP, executorID string) error {
	lrp.State = models.ActualLRPStateStarting
	lrp.Since = bbs.timeProvider.Time().UnixNano()
	lrp.ExecutorID = executorID

	return shared.RetryIndefinitelyOnStoreTimeout(func() error {
		return bbs.store.SetMulti([]storeadapter.StoreNode{
			{
				Key:   shared.ActualLRPSchemaPath(lrp),
				Value: lrp.ToJSON(),
			},
		})
	})
}

func (bbs *LRPBBS) ReportActualLRPAsRunning(lrp models.ActualLRP, executorID string) error {
	lrp.State = models.ActualLRPStateRunning
	lrp.Since = bbs.timeProvider.Time().UnixNano()
	lrp.ExecutorID = executorID

	return shared.RetryIndefinitelyOnStoreTimeout(func() error {
		return bbs.store.SetMulti([]storeadapter.StoreNode{
			{
				Key:   shared.ActualLRPSchemaPath(lrp),
				Value: lrp.ToJSON(),
			},
		})
	})
}
