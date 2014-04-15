package fake_bbs

import (
	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

type FakeMetricsBBS struct {
	GetAllRunOncesReturns struct {
		Models []*models.RunOnce
		Err    error
	}
}

func NewFakeMetricsBBS() *FakeMetricsBBS {
	return &FakeMetricsBBS{}
}

func (bbs *FakeMetricsBBS) GetAllRunOnces() ([]*models.RunOnce, error) {
	return bbs.GetAllRunOncesReturns.Models, bbs.GetAllRunOncesReturns.Err
}
