package services_bbs

import (
	"errors"
	"time"

	"github.com/cloudfoundry/storeadapter"
)

type Presence interface {
	Maintain(interval time.Duration) (status <-chan bool, err error)
	Remove()
}

type presence struct {
	store   storeadapter.StoreAdapter
	key     string
	value   []byte
	release chan chan bool
}

func NewPresence(store storeadapter.StoreAdapter, key string, value []byte) Presence {
	return &presence{
		store: store,
		key:   key,
		value: value,
	}
}

func (p *presence) Maintain(interval time.Duration) (<-chan bool, error) {
	if p.release != nil {
		return nil, errors.New("Already maintaining a presence")
	}

	status, release, err := p.store.MaintainNode(storeadapter.StoreNode{
		Key:   p.key,
		Value: p.value,
		TTL:   uint64(interval.Seconds()),
	})

	if err != nil {
		return nil, err
	}

	p.release = release

	return status, nil
}

func (p *presence) Remove() {
	if p.release == nil {
		return
	}

	release := p.release
	p.release = nil

	stopFinishedChan := make(chan bool)
	release <- stopFinishedChan

	<-stopFinishedChan
}
