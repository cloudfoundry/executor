package bbs

import (
	"errors"
	"github.com/cloudfoundry/storeadapter"
	"time"
)

type PresenceInterface interface {
	Remove()
}

type Presence struct {
	store   storeadapter.StoreAdapter
	key     string
	value   []byte
	release chan chan bool
}

func NewPresence(store storeadapter.StoreAdapter, key string, value []byte) *Presence {
	return &Presence{
		store: store,
		key:   key,
		value: value,
	}
}

func (p *Presence) Maintain(interval time.Duration) (<-chan bool, error) {
	if p.release != nil {
		return nil, errors.New("Already maintaining a presence")
	}

	lost, release, err := p.store.MaintainNode(storeadapter.StoreNode{
		Key:   p.key,
		Value: p.value,
		TTL:   uint64(interval.Seconds()),
	})

	if err != nil {
		return nil, err
	}

	p.release = release

	return lost, nil
}

func (p *Presence) Remove() {
	if p.release == nil {
		return
	}

	release := p.release
	p.release = nil

	stopFinishedChan := make(chan bool)
	release <- stopFinishedChan

	<-stopFinishedChan
}
