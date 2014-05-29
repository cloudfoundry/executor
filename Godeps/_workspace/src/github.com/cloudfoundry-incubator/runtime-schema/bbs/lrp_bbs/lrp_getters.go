package lrp_bbs

import (
	"fmt"
	"path"

	"github.com/cloudfoundry-incubator/runtime-schema/bbs/shared"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/storeadapter"
)

func (bbs *LRPBBS) GetAllDesiredLRPs() ([]models.DesiredLRP, error) {
	lrps := []models.DesiredLRP{}

	node, err := bbs.store.ListRecursively(shared.DesiredLRPSchemaRoot)
	if err == storeadapter.ErrorKeyNotFound {
		return lrps, nil
	}

	if err != nil {
		return lrps, err
	}

	for _, node := range node.ChildNodes {
		lrp, err := models.NewDesiredLRPFromJSON(node.Value)
		if err != nil {
			return lrps, fmt.Errorf("cannot parse lrp JSON for key %s: %s", node.Key, err.Error())
		} else {
			lrps = append(lrps, lrp)
		}
	}

	return lrps, nil
}

func (bbs *LRPBBS) GetDesiredLRPByProcessGuid(processGuid string) (models.DesiredLRP, error) {
	node, err := bbs.store.Get(shared.DesiredLRPSchemaPath(models.DesiredLRP{
		ProcessGuid: processGuid,
	}))
	if err != nil {
		return models.DesiredLRP{}, err
	}
	return models.NewDesiredLRPFromJSON(node.Value)
}

func (bbs *LRPBBS) GetAllActualLRPs() ([]models.ActualLRP, error) {
	lrps := []models.ActualLRP{}

	node, err := bbs.store.ListRecursively(shared.ActualLRPSchemaRoot)
	if err == storeadapter.ErrorKeyNotFound {
		return lrps, nil
	}

	if err != nil {
		return lrps, err
	}

	for _, node := range node.ChildNodes {
		for _, indexNode := range node.ChildNodes {
			for _, instanceNode := range indexNode.ChildNodes {
				lrp, err := models.NewActualLRPFromJSON(instanceNode.Value)
				if err != nil {
					return lrps, fmt.Errorf("cannot parse lrp JSON for key %s: %s", instanceNode.Key, err.Error())
				} else {
					lrps = append(lrps, lrp)
				}
			}
		}
	}

	return lrps, nil
}

func (bbs *LRPBBS) GetRunningActualLRPs() ([]models.ActualLRP, error) {
	lrps, err := bbs.GetAllActualLRPs()
	if err != nil {
		return []models.ActualLRP{}, err
	}

	return filterActualLRPs(lrps, models.ActualLRPStateRunning), nil
}

func (bbs *LRPBBS) GetActualLRPsByProcessGuid(processGuid string) ([]models.ActualLRP, error) {
	lrps := []models.ActualLRP{}

	node, err := bbs.store.ListRecursively(path.Join(shared.ActualLRPSchemaRoot, processGuid))
	if err == storeadapter.ErrorKeyNotFound {
		return lrps, nil
	}

	if err != nil {
		return lrps, err
	}

	for _, indexNode := range node.ChildNodes {
		for _, instanceNode := range indexNode.ChildNodes {
			lrp, err := models.NewActualLRPFromJSON(instanceNode.Value)
			if err != nil {
				return lrps, fmt.Errorf("cannot parse lrp JSON for key %s: %s", instanceNode.Key, err.Error())
			} else {
				lrps = append(lrps, lrp)
			}
		}
	}

	return lrps, nil
}

func (bbs *LRPBBS) GetRunningActualLRPsByProcessGuid(processGuid string) ([]models.ActualLRP, error) {
	lrps, err := bbs.GetActualLRPsByProcessGuid(processGuid)
	if err != nil {
		return []models.ActualLRP{}, err
	}

	return filterActualLRPs(lrps, models.ActualLRPStateRunning), nil
}

func filterActualLRPs(lrps []models.ActualLRP, state models.ActualLRPState) []models.ActualLRP {
	filteredLRPs := []models.ActualLRP{}
	for _, lrp := range lrps {
		if lrp.State == state {
			filteredLRPs = append(filteredLRPs, lrp)
		}
	}

	return filteredLRPs
}
