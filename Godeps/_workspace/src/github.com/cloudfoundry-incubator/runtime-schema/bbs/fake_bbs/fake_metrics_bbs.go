package fake_bbs

import (
	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

type FakeMetricsBBS struct {
	GetAllTasksReturns struct {
		Models []models.Task
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

func (bbs *FakeMetricsBBS) GetAllTasks() ([]models.Task, error) {
	return bbs.GetAllTasksReturns.Models, bbs.GetAllTasksReturns.Err
}

func (bbs *FakeMetricsBBS) GetServiceRegistrations() (models.ServiceRegistrations, error) {
	return bbs.GetServiceRegistrationsReturns.Registrations, bbs.GetServiceRegistrationsReturns.Err
}
