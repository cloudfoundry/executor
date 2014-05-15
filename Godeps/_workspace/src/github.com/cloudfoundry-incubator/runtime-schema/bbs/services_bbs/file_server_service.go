package services_bbs

import (
	"errors"
	"math/rand"

	"github.com/cloudfoundry-incubator/runtime-schema/bbs/shared"
)

func (self *ServicesBBS) GetAvailableFileServer() (string, error) {
	node, err := self.store.ListRecursively(shared.FileServerSchemaRoot)
	if err != nil {
		return "", err
	}

	if len(node.ChildNodes) == 0 {
		return "", errors.New("No file servers are currently available")
	}

	randomServerIndex := rand.Intn(len(node.ChildNodes))
	return string(node.ChildNodes[randomServerIndex].Value), nil
}
