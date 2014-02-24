package bbs

import (
	"errors"
	"github.com/cloudfoundry/storeadapter"
	"time"
)

type PresenceInterface interface {
	Remove() error
}

type Presence struct {
	store    storeadapter.StoreAdapter
	key      string
	value    []byte
	stopChan chan chan bool
}

func NewPresence(store storeadapter.StoreAdapter, key string, value []byte) *Presence {
	return &Presence{
		store: store,
		key:   key,
		value: value,
	}
}

func (p *Presence) Maintain(interval uint64) (chan error, error) {
	if p.stopChan != nil {
		return nil, errors.New("Already maintaining a presence")
	}

	err := p.store.SetMulti([]storeadapter.StoreNode{
		{
			Key:   p.key,
			Value: p.value,
			TTL:   interval,
		},
	})

	if err != nil {
		return nil, err
	}

	stopChan := make(chan chan bool)
	p.stopChan = stopChan
	errors := make(chan error, 1)

	go func() {
		ticker := time.NewTicker(time.Duration(interval) * time.Second / 2)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				err := p.store.Update(storeadapter.StoreNode{
					Key:   p.key,
					Value: p.value,
					TTL:   interval,
				})

				if err != nil {
					close(stopChan)
					p.stopChan = nil

					errors <- err
					return
				}
			case stopFinishedChan := <-stopChan:
				stopFinishedChan <- true
				return
			}
		}
	}()

	return errors, nil
}

func (p *Presence) Remove() error {
	if p.stopChan == nil {
		return nil
	}

	stopChan := p.stopChan
	p.stopChan = nil

	stopFinishedChan := make(chan bool)
	stopChan <- stopFinishedChan

	<-stopFinishedChan

	err := p.store.Delete(p.key)
	return err
}
