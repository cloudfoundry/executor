package bbs

import (
	"fmt"
	"path"
	"time"

	"github.com/cloudfoundry/storeadapter"
)

const CCSchemaRoot = SchemaRoot + "cloud_controller"

func ccRegistrationKey(host string, port int) string {
	key := fmt.Sprintf("%s:%d", host, port)
	return path.Join(CCSchemaRoot, key)
}

func ccRegistrationValue(host string, port int) []byte {
	return []byte(fmt.Sprintf("http://%s:%d", host, port))
}

type servistryBBS struct {
	store storeadapter.StoreAdapter
}

func (bbs *servistryBBS) RegisterCC(host string, port int, ttl time.Duration) error {
	ccNode := storeadapter.StoreNode{
		Key:   ccRegistrationKey(host, port),
		Value: ccRegistrationValue(host, port),
		TTL:   uint64(ttl.Seconds()),
	}

	err := bbs.store.Update(ccNode)
	if err == storeadapter.ErrorKeyNotFound {
		err = bbs.store.Create(ccNode)
	}

	return err
}

func (bbs *servistryBBS) UnregisterCC(host string, port int) error {
	err := bbs.store.Delete(ccRegistrationKey(host, port))
	if err == storeadapter.ErrorKeyNotFound {
		return nil
	}
	return err
}

func (bbs *servistryBBS) GetAvailableCC() ([]string, error) {
	parentNode, err := bbs.store.ListRecursively(CCSchemaRoot)
	if err != nil {
		return []string{}, err
	}

	urls := []string{}
	for _, ccNode := range parentNode.ChildNodes {
		urls = append(urls, string(ccNode.Value))
	}
	return urls, nil
}
