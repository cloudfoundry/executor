package keyed_lock

import (
	"fmt"
	"sync"

	"github.com/pivotal-golang/lager"
)

//go:generate counterfeiter -o fakelockmanager/fake_lock_manager.go . LockManager
type LockManager interface {
	Lock(logger lager.Logger, key string)
	Unlock(logger lager.Logger, key string)
}

type lockManager struct {
	locks map[string]*lockEntry
	mutex sync.Mutex
}

type lockEntry struct {
	ch    chan struct{}
	count int
}

func NewLockManager() LockManager {
	locks := map[string]*lockEntry{}
	return &lockManager{
		locks: locks,
	}
}

func (m *lockManager) Lock(logger lager.Logger, key string) {
	logger.Debug("locking")
	defer logger.Debug("locking-complete")
	m.mutex.Lock()
	entry, ok := m.locks[key]
	if !ok {
		entry = &lockEntry{
			ch: make(chan struct{}, 1),
		}
		m.locks[key] = entry
	}

	entry.count++
	m.mutex.Unlock()
	entry.ch <- struct{}{}
}

func (m *lockManager) Unlock(logger lager.Logger, key string) {
	logger.Debug("unlocking")
	defer logger.Debug("unlocking-complete")

	m.mutex.Lock()
	entry, ok := m.locks[key]
	if !ok {
		panic(fmt.Sprintf("key %q already unlocked", key))
	}

	entry.count--
	if entry.count == 0 {
		delete(m.locks, key)
	}

	m.mutex.Unlock()
	<-entry.ch
}
