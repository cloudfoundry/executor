package bbs

import (
	"fmt"
	"path"
	"time"

	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/storeadapter"
)

const CCSchemaRoot = SchemaRoot + "cloud_controller"

func ccRegistrationKey(registration models.CCRegistrationMessage) string {
	key := fmt.Sprintf("%s:%d", registration.Host, registration.Port)
	return path.Join(CCSchemaRoot, key)
}

func ccRegistrationValue(registration models.CCRegistrationMessage) []byte {
	return []byte(fmt.Sprintf("http://%s:%d", registration.Host, registration.Port))
}

type servistryBBS struct {
	store storeadapter.StoreAdapter
}

func (bbs *servistryBBS) RegisterCC(registration models.CCRegistrationMessage, ttl time.Duration) error {
	ccNode := storeadapter.StoreNode{
		Key:   ccRegistrationKey(registration),
		Value: ccRegistrationValue(registration),
		TTL:   uint64(ttl.Seconds()),
	}

	err := bbs.store.Update(ccNode)
	if err == storeadapter.ErrorKeyNotFound {
		err = bbs.store.Create(ccNode)
	}

	return err
}

func (bbs *servistryBBS) UnregisterCC(registration models.CCRegistrationMessage) error {
	err := bbs.store.Delete(ccRegistrationKey(registration))
	if err == storeadapter.ErrorKeyNotFound {
		return nil
	}
	return err
}
