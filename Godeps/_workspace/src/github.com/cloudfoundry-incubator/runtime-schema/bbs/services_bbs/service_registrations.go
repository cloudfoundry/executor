package services_bbs

import (
	"path"

	"github.com/cloudfoundry-incubator/runtime-schema/bbs/shared"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/storeadapter"
)

var serviceSchemas = map[string]string{
	models.ExecutorServiceName:   shared.ExecutorSchemaRoot,
	models.FileServerServiceName: shared.FileServerSchemaRoot,
}

func (bbs *ServicesBBS) GetServiceRegistrations() (models.ServiceRegistrations, error) {
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

func (bbs *ServicesBBS) registrationsForServiceName(name string) (models.ServiceRegistrations, error) {
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

func (bbs *ServicesBBS) GetAllExecutors() ([]string, error) {
	nodes, err := bbs.store.ListRecursively(shared.ExecutorSchemaRoot)
	if err == storeadapter.ErrorKeyNotFound {
		return []string{}, nil
	} else if err != nil {
		return nil, err
	}

	executors := []string{}

	for _, node := range nodes.ChildNodes {
		executors = append(executors, node.KeyComponents()[2])
	}

	return executors, nil
}
