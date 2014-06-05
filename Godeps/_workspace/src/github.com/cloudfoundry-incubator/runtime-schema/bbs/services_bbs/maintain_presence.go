package services_bbs

import (
	"time"

	"github.com/cloudfoundry-incubator/runtime-schema/bbs/shared"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

func (bbs *ServicesBBS) MaintainExecutorPresence(heartbeatInterval time.Duration, executorId string) (Presence, <-chan bool, error) {
	presence := NewPresence(bbs.store, shared.ExecutorSchemaPath(executorId), []byte{})
	status, err := presence.Maintain(heartbeatInterval)
	return presence, status, err
}

func (bbs *ServicesBBS) MaintainRepPresence(heartbeatInterval time.Duration, repPresence models.RepPresence) (Presence, <-chan bool, error) {
	presence := NewPresence(bbs.store, shared.RepSchemaPath(repPresence.RepID), repPresence.ToJSON())
	status, err := presence.Maintain(heartbeatInterval)
	return presence, status, err
}

func (bbs *ServicesBBS) MaintainFileServerPresence(heartbeatInterval time.Duration, fileServerURL string, fileServerId string) (Presence, <-chan bool, error) {
	key := shared.FileServerSchemaPath(fileServerId)
	presence := NewPresence(bbs.store, key, []byte(fileServerURL))
	status, err := presence.Maintain(heartbeatInterval)
	return presence, status, err
}
