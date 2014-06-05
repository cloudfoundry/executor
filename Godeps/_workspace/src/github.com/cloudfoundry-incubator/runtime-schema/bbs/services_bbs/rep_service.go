package services_bbs

import (
	"github.com/cloudfoundry-incubator/runtime-schema/bbs/shared"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/storeadapter"
)

func (bbs *ServicesBBS) GetAllReps() ([]models.RepPresence, error) {
	node, err := bbs.store.ListRecursively(shared.RepSchemaRoot)
	if err == storeadapter.ErrorKeyNotFound {
		return []models.RepPresence{}, nil
	}
	if err != nil {
		return nil, err
	}

	var repPresences []models.RepPresence
	for _, node := range node.ChildNodes {
		repPresence, err := models.NewRepPresenceFromJSON(node.Value)
		if err != nil {
			bbs.logger.Errord(map[string]interface{}{
				"error": err.Error(),
			}, "bbs.get-all-reps.invalid-json")
			continue
		}
		repPresences = append(repPresences, repPresence)
	}
	return repPresences, nil
}
