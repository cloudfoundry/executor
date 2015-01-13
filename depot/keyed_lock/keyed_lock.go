package keyed_lock

import (
	"fmt"
	"sync"
)

type LockManager struct {
	locks map[string]*lockEntry
	mutex sync.Mutex
}

type lockEntry struct {
	ch    chan struct{}
	count int
}

func NewLockManager() *LockManager {
	locks := map[string]*lockEntry{}
	return &LockManager{
		locks: locks,
	}
}

func (m *LockManager) Lock(key string) {
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

func (m *LockManager) Unlock(key string) {
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
