package fake_bbs

import (
	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

type FakeMetricsBBS struct {
	GetAllRunOncesReturns struct {
		Models []*models.RunOnce
		Err    error
	}

	GetServiceRegistrationsReturns struct {
		Registrations models.ServiceRegistrations
		Err           error
	}
}

func NewFakeMetricsBBS() *FakeMetricsBBS {
	return &FakeMetricsBBS{}
}

func (bbs *FakeMetricsBBS) GetAllRunOnces() ([]*models.RunOnce, error) {
	return bbs.GetAllRunOncesReturns.Models, bbs.GetAllRunOncesReturns.Err
}

func (bbs *FakeMetricsBBS) GetServiceRegistrations() (models.ServiceRegistrations, error) {
	return bbs.GetServiceRegistrationsReturns.Registrations, bbs.GetServiceRegistrationsReturns.Err
}
