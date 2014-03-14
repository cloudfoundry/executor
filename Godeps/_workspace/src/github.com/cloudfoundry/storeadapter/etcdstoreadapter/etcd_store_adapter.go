package etcdstoreadapter

import (
	"github.com/cloudfoundry/storeadapter"
	"github.com/cloudfoundry/storeadapter/workerpool"
	"github.com/coreos/go-etcd/etcd"
	"github.com/nu7hatch/gouuid"
	"sync"
	"time"
)

type ETCDStoreAdapter struct {
	urls              []string
	client            *etcd.Client
	workerPool        *workerpool.WorkerPool
	inflightWatches   map[chan bool]bool
	inflightWatchLock *sync.Mutex
}

func NewETCDStoreAdapter(urls []string, workerPool *workerpool.WorkerPool) *ETCDStoreAdapter {
	return &ETCDStoreAdapter{
		urls:              urls,
		workerPool:        workerPool,
		inflightWatches:   map[chan bool]bool{},
		inflightWatchLock: &sync.Mutex{},
	}
}

func (adapter *ETCDStoreAdapter) Connect() error {
	adapter.client = etcd.NewClient(adapter.urls)

	return nil
}

func (adapter *ETCDStoreAdapter) Disconnect() error {
	adapter.workerPool.StopWorkers()
	adapter.cancelInflightWatches()

	return nil
}

func (adapter *ETCDStoreAdapter) isEventIndexClearedError(err error) bool {
	return adapter.etcdErrorCode(err) == 401
}

func (adapter *ETCDStoreAdapter) etcdErrorCode(err error) int {
	if err != nil {
		switch err.(type) {
		case etcd.EtcdError:
			return err.(etcd.EtcdError).ErrorCode
		case *etcd.EtcdError:
			return err.(*etcd.EtcdError).ErrorCode
		}
	}
	return 0
}

func (adapter *ETCDStoreAdapter) convertError(err error) error {
	switch adapter.etcdErrorCode(err) {
	case 501:
		return storeadapter.ErrorTimeout
	case 100:
		return storeadapter.ErrorKeyNotFound
	case 102:
		return storeadapter.ErrorNodeIsDirectory
	case 105:
		return storeadapter.ErrorKeyExists
	}

	return err
}

func (adapter *ETCDStoreAdapter) SetMulti(nodes []storeadapter.StoreNode) error {
	results := make(chan error, len(nodes))

	for _, node := range nodes {
		node := node
		adapter.workerPool.ScheduleWork(func() {
			_, err := adapter.client.Set(node.Key, string(node.Value), node.TTL)
			results <- err
		})
	}

	var err error
	numReceived := 0
	for numReceived < len(nodes) {
		result := <-results
		numReceived++
		if err == nil {
			err = result
		}
	}

	return adapter.convertError(err)
}

func (adapter *ETCDStoreAdapter) Get(key string) (storeadapter.StoreNode, error) {
	done := make(chan bool, 1)
	var response *etcd.Response
	var err error

	//we route through the worker pool to enable usage tracking
	adapter.workerPool.ScheduleWork(func() {
		response, err = adapter.client.Get(key, false, false)
		done <- true
	})

	<-done

	if err != nil {
		return storeadapter.StoreNode{}, adapter.convertError(err)
	}

	if response.Node.Dir {
		return storeadapter.StoreNode{}, storeadapter.ErrorNodeIsDirectory
	}

	return storeadapter.StoreNode{
		Key:   response.Node.Key,
		Value: []byte(response.Node.Value),
		Dir:   response.Node.Dir,
		TTL:   uint64(response.Node.TTL),
	}, nil
}

func (adapter *ETCDStoreAdapter) ListRecursively(key string) (storeadapter.StoreNode, error) {
	done := make(chan bool, 1)
	var response *etcd.Response
	var err error

	//we route through the worker pool to enable usage tracking
	adapter.workerPool.ScheduleWork(func() {
		response, err = adapter.client.Get(key, false, true)
		done <- true
	})

	<-done

	if err != nil {
		return storeadapter.StoreNode{}, adapter.convertError(err)
	}

	if !response.Node.Dir {
		return storeadapter.StoreNode{}, storeadapter.ErrorNodeIsNotDirectory
	}

	if len(response.Node.Nodes) == 0 {
		return storeadapter.StoreNode{Key: key, Dir: true, Value: []byte{}, ChildNodes: []storeadapter.StoreNode{}}, nil
	}

	return adapter.makeStoreNode(*response.Node), nil
}

func (adapter *ETCDStoreAdapter) Create(node storeadapter.StoreNode) error {
	results := make(chan error, 1)

	adapter.workerPool.ScheduleWork(func() {
		_, err := adapter.client.Create(node.Key, string(node.Value), node.TTL)
		results <- err
	})

	return adapter.convertError(<-results)
}

func (adapter *ETCDStoreAdapter) Update(node storeadapter.StoreNode) error {
	results := make(chan error, 1)

	adapter.workerPool.ScheduleWork(func() {
		_, err := adapter.client.Update(node.Key, string(node.Value), node.TTL)
		results <- err
	})

	return adapter.convertError(<-results)
}

func (adapter *ETCDStoreAdapter) Delete(keys ...string) error {
	results := make(chan error, len(keys))

	for _, key := range keys {
		key := key
		adapter.workerPool.ScheduleWork(func() {
			_, err := adapter.client.Delete(key, true)
			results <- err
		})
	}

	var err error
	numReceived := 0
	for numReceived < len(keys) {
		result := <-results
		numReceived++
		if err == nil {
			err = result
		}
	}

	return adapter.convertError(err)
}

func (adapter *ETCDStoreAdapter) UpdateDirTTL(key string, ttl uint64) error {
	response, err := adapter.Get(key)
	if err == nil && response.Dir == false {
		return storeadapter.ErrorNodeIsNotDirectory
	}

	results := make(chan error, 1)

	adapter.workerPool.ScheduleWork(func() {
		_, err = adapter.client.UpdateDir(key, ttl)
		results <- err
	})

	return adapter.convertError(<-results)
}

func (adapter *ETCDStoreAdapter) Watch(key string) (<-chan storeadapter.WatchEvent, chan<- bool, <-chan error) {
	events := make(chan storeadapter.WatchEvent)
	errors := make(chan error)
	stop := make(chan bool, 1)

	go adapter.dispatchWatchEvents(key, events, stop, errors)

	time.Sleep(100 * time.Millisecond) //give the watcher a chance to connect

	return events, stop, errors
}

func (adapter *ETCDStoreAdapter) dispatchWatchEvents(key string, events chan<- storeadapter.WatchEvent, stop chan bool, errors chan<- error) {
	var index uint64
	adapter.registerInflightWatch(stop)

	defer close(events)
	defer close(errors)
	defer adapter.unregisterInflightWatch(stop)

	for {
		response, err := adapter.client.Watch(key, index, true, nil, stop)
		if err != nil {
			if adapter.isEventIndexClearedError(err) {
				index++
				continue
			} else if err == etcd.ErrWatchStoppedByUser {
				return
			} else {
				errors <- adapter.convertError(err)
				return
			}
		}

		events <- adapter.makeWatchEvent(response)
		index = response.Node.ModifiedIndex + 1
	}
}

func (adapter *ETCDStoreAdapter) registerInflightWatch(stop chan bool) {
	adapter.inflightWatchLock.Lock()
	defer adapter.inflightWatchLock.Unlock()
	adapter.inflightWatches[stop] = true
}

func (adapter *ETCDStoreAdapter) unregisterInflightWatch(stop chan bool) {
	adapter.inflightWatchLock.Lock()
	defer adapter.inflightWatchLock.Unlock()
	delete(adapter.inflightWatches, stop)
}

func (adapter *ETCDStoreAdapter) cancelInflightWatches() {
	adapter.inflightWatchLock.Lock()
	defer adapter.inflightWatchLock.Unlock()
	for stop := range adapter.inflightWatches {
		close(stop)
	}
}

func (adapter *ETCDStoreAdapter) makeStoreNode(etcdNode etcd.Node) storeadapter.StoreNode {
	if etcdNode.Dir {
		node := storeadapter.StoreNode{
			Key:        etcdNode.Key,
			Dir:        true,
			Value:      []byte{},
			ChildNodes: []storeadapter.StoreNode{},
			TTL:        uint64(etcdNode.TTL),
		}

		for _, child := range etcdNode.Nodes {
			node.ChildNodes = append(node.ChildNodes, adapter.makeStoreNode(child))
		}

		return node
	} else {
		return storeadapter.StoreNode{
			Key:   etcdNode.Key,
			Value: []byte(etcdNode.Value),
			TTL:   uint64(etcdNode.TTL),
		}
	}
}

func (adapter *ETCDStoreAdapter) makeWatchEvent(event *etcd.Response) storeadapter.WatchEvent {
	var eventType storeadapter.EventType
	var node *etcd.Node

	switch event.Action {
	case "delete":
		eventType = storeadapter.DeleteEvent
		node = event.PrevNode
	case "create":
		eventType = storeadapter.CreateEvent
		node = event.Node
	case "set", "update":
		eventType = storeadapter.UpdateEvent
		node = event.Node
	case "expire":
		eventType = storeadapter.ExpireEvent
		node = event.PrevNode
	}
	return storeadapter.WatchEvent{
		Type: eventType,
		Node: adapter.makeStoreNode(*node),
	}
}

func (adapter *ETCDStoreAdapter) MaintainNode(storeNode storeadapter.StoreNode) (lostNode <-chan bool, releaseNode chan (chan bool), err error) {
	if storeNode.TTL == 0 {
		return nil, nil, storeadapter.ErrorInvalidTTL
	}

	if len(storeNode.Value) == 0 {
		guid, err := uuid.NewV4()
		if err != nil {
			return nil, nil, err
		}

		storeNode.Value = []byte(guid.String())
	}

	releaseNodeChannel := make(chan chan bool)
	lostNodeChannel := make(chan bool)

	for {
		err := adapter.Create(storeNode)
		convertedError := adapter.convertError(err)
		if convertedError == storeadapter.ErrorTimeout {
			return nil, nil, storeadapter.ErrorTimeout
		}

		if err == nil {
			break
		}

		time.Sleep(1 * time.Second)
	}

	go adapter.maintainNode(storeNode, lostNodeChannel, releaseNodeChannel)

	return lostNodeChannel, releaseNodeChannel, nil
}

func (adapter *ETCDStoreAdapter) maintainNode(storeNode storeadapter.StoreNode, lostNodeChannel chan bool, releaseNodeChannel chan (chan bool)) {
	maintenanceInterval := time.Duration(storeNode.TTL) * time.Second / time.Duration(2)
	ticker := time.NewTicker(maintenanceInterval)
	for {
		select {
		case <-ticker.C:
			_, err := adapter.client.CompareAndSwap(storeNode.Key, string(storeNode.Value), storeNode.TTL, string(storeNode.Value), 0)
			if err != nil {
				lostNodeChannel <- true
			}
		case released := <-releaseNodeChannel:
			adapter.client.CompareAndDelete(storeNode.Key, string(storeNode.Value), 0)
			close(released)
			return
		}
	}
}
