package bbs

import (
	"path"

	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/cloudfoundry/storeadapter"
)

var serviceSchemas = map[string]string{
	models.ExecutorServiceName:   ExecutorSchemaRoot,
	models.FileServerServiceName: FileServerSchemaRoot,
}

type metricsBBS struct {
	store storeadapter.StoreAdapter
}

func (bbs *metricsBBS) GetAllTasks() ([]models.Task, error) {
	node, err := bbs.store.ListRecursively(TaskSchemaRoot)
	if err == storeadapter.ErrorKeyNotFound {
		return []models.Task{}, nil
	}

	if err != nil {
		return []models.Task{}, err
	}

	tasks := []models.Task{}
	for _, node := range node.ChildNodes {
		task, err := models.NewTaskFromJSON(node.Value)
		if err != nil {
			steno.NewLogger("bbs").Errorf("cannot parse task JSON for key %s: %s", node.Key, err.Error())
		} else {
			tasks = append(tasks, task)
		}
	}

	return tasks, nil
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
