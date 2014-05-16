package services_bbs

import (
	"time"

	"github.com/cloudfoundry-incubator/runtime-schema/bbs/shared"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

func (self *ServicesBBS) MaintainExecutorPresence(heartbeatInterval time.Duration, executorId string) (Presence, <-chan bool, error) {
	presence := NewPresence(self.store, shared.ExecutorSchemaPath(executorId), []byte{})
	status, err := presence.Maintain(heartbeatInterval)
	return presence, status, err
}

func (self *ServicesBBS) MaintainRepPresence(heartbeatInterval time.Duration, repPresence models.RepPresence) (Presence, <-chan bool, error) {
	presence := NewPresence(self.store, shared.RepSchemaPath(repPresence.RepID), repPresence.ToJSON())
	status, err := presence.Maintain(heartbeatInterval)
	return presence, status, err
}

func (self *ServicesBBS) MaintainFileServerPresence(heartbeatInterval time.Duration, fileServerURL string, fileServerId string) (Presence, <-chan bool, error) {
	key := shared.FileServerSchemaPath(fileServerId)
	presence := NewPresence(self.store, key, []byte(fileServerURL))
	status, err := presence.Maintain(heartbeatInterval)
	return presence, status, err
}
