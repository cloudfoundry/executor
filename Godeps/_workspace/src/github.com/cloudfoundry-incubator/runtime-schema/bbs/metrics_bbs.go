package bbs

import (
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/cloudfoundry/storeadapter"
	"path"
)

var serviceSchemas = map[string]string{
	models.ExecutorServiceName:   ExecutorSchemaRoot,
	models.FileServerServiceName: FileServerSchemaRoot,
}

type metricsBBS struct {
	store storeadapter.StoreAdapter
}

func (bbs *metricsBBS) GetAllRunOnces() ([]*models.RunOnce, error) {
	node, err := bbs.store.ListRecursively(RunOnceSchemaRoot)
	if err == storeadapter.ErrorKeyNotFound {
		return []*models.RunOnce{}, nil
	}

	if err != nil {
		return []*models.RunOnce{}, err
	}

	runOnces := []*models.RunOnce{}
	for _, node := range node.ChildNodes {
		runOnce, err := models.NewRunOnceFromJSON(node.Value)
		if err != nil {
			steno.NewLogger("bbs").Errorf("cannot parse runOnce JSON for key %s: %s", node.Key, err.Error())
		} else {
			runOnces = append(runOnces, &runOnce)
		}
	}

	return runOnces, nil
}

func (bbs *metricsBBS) GetServiceRegistrations() (models.ServiceRegistrations, error) {
	registrations := models.ServiceRegistrations{}

	for serviceName := range serviceSchemas {
		serviceRegistrations, err := bbs.registrationsForServiceName(serviceName)
		if err != nil {
			return models.ServiceRegistrations{}, err
		}
		registrations = append(registrations, serviceRegistrations...)
	}

	return registrations, nil
}

func (bbs *metricsBBS) registrationsForServiceName(name string) (models.ServiceRegistrations, error) {
	registrations := models.ServiceRegistrations{}

	rootNode, err := bbs.store.ListRecursively(serviceSchemas[name])
	if err == storeadapter.ErrorKeyNotFound {
		return registrations, nil
	} else if err != nil {
		return registrations, err
	}

	for _, node := range rootNode.ChildNodes {
		reg := models.ServiceRegistration{
			Name:     name,
			Id:       path.Base(node.Key),
			Location: string(node.Value),
		}
		registrations = append(registrations, reg)
	}

	return registrations, nil
}
