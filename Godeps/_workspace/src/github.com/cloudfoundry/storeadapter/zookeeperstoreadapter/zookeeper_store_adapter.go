package zookeeperstoreadapter

import (
	"fmt"
	"github.com/cloudfoundry/gunk/timeprovider"
	"github.com/cloudfoundry/storeadapter"
	"github.com/cloudfoundry/storeadapter/workerpool"
	"github.com/samuel/go-zookeeper/zk"
	"math"
	"strconv"

	"path"
	"strings"
	"time"
)

type fetchedNode struct {
	node      storeadapter.StoreNode
	err       error
	isExpired bool
}

type ZookeeperStoreAdapter struct {
	urls              []string
	client            *zk.Conn
	workerPool        *workerpool.WorkerPool
	timeProvider      timeprovider.TimeProvider
	connectionTimeout time.Duration
}

func NewZookeeperStoreAdapter(urls []string, workerPool *workerpool.WorkerPool, timeProvider timeprovider.TimeProvider, connectionTimeout time.Duration) *ZookeeperStoreAdapter {
	return &ZookeeperStoreAdapter{
		urls:              urls,
		workerPool:        workerPool,
		timeProvider:      timeProvider,
		connectionTimeout: connectionTimeout,
	}
}

func (adapter *ZookeeperStoreAdapter) Connect() error {
	var err error
	adapter.client, _, err = zk.Connect(adapter.urls, adapter.connectionTimeout)
	return err
}

func (adapter *ZookeeperStoreAdapter) Disconnect() error {
	adapter.workerPool.StopWorkers()
	adapter.client.Close()

	return nil
}

func (adapter *ZookeeperStoreAdapter) SetMulti(nodes []storeadapter.StoreNode) error {
	results := make(chan error, len(nodes))
	for _, node := range nodes {
		node := node
		adapter.workerPool.ScheduleWork(func() {
			var err error

			exists, stat, err := adapter.client.Exists(node.Key)
			if err != nil {
				results <- err
				return
			}
			if stat.NumChildren > 0 {
				results <- storeadapter.ErrorNodeIsDirectory
				return
			}

			if exists {
				_, err = adapter.client.Set(node.Key, adapter.encode(node.Value, node.TTL, adapter.timeProvider.Time()), -1)
			} else {
				err = adapter.createNode(node)
			}

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

	if adapter.isTimeoutError(err) {
		return storeadapter.ErrorTimeout
	}

	return err
}

func (adapter *ZookeeperStoreAdapter) Get(key string) (node storeadapter.StoreNode, err error) {
	fetchedNode := adapter.getWithTTLPolicy(key)

	if fetchedNode.err != nil {
		return storeadapter.StoreNode{}, fetchedNode.err
	}

	if fetchedNode.isExpired {
		return storeadapter.StoreNode{}, storeadapter.ErrorKeyNotFound
	}

	if fetchedNode.node.Dir {
		return storeadapter.StoreNode{}, storeadapter.ErrorNodeIsDirectory
	}

	return fetchedNode.node, nil
}

func (adapter *ZookeeperStoreAdapter) ListRecursively(key string) (storeadapter.StoreNode, error) {
	nodeKeys, _, err := adapter.client.Children(key)

	if adapter.isTimeoutError(err) {
		return storeadapter.StoreNode{}, storeadapter.ErrorTimeout
	}

	if adapter.isMissingKeyError(err) {
		return storeadapter.StoreNode{}, storeadapter.ErrorKeyNotFound
	}

	if err != nil {
		return storeadapter.StoreNode{}, err
	}

	if key == "/" {
		nodeKeys = adapter.pruneZookeepersInternalNodeKeys(nodeKeys)
	}

	if len(nodeKeys) == 0 {
		if adapter.isNodeDirectory(key) {
			return storeadapter.StoreNode{Key: key, Dir: true, ChildNodes: []storeadapter.StoreNode{}}, nil
		} else {
			return storeadapter.StoreNode{}, storeadapter.ErrorNodeIsNotDirectory
		}
	}

	childNodes, err := adapter.getMultipleNodesSimultaneously(key, nodeKeys)

	if err != nil {
		return storeadapter.StoreNode{}, err
	}

	//This could be done concurrently too
	//if zookeeper's recursive read performance proves to be slow
	//we could simply launch each of these ListRecursively's in a map-reduce
	//fashion
	for i, node := range childNodes {
		if node.Dir == true {
			listedNode, err := adapter.ListRecursively(node.Key)
			if err != nil {
				return storeadapter.StoreNode{}, err
			}
			childNodes[i] = listedNode
		}
	}

	return storeadapter.StoreNode{
		Key:        key,
		Dir:        true,
		ChildNodes: childNodes,
	}, nil
}

func (adapter *ZookeeperStoreAdapter) Delete(keys ...string) error {
	//NOTE: this can be optimized if we choose to go with zookeeper (can use the worker pool)
	var finalErr error
	for _, key := range keys {
		exists, stat, err := adapter.client.Exists(key)
		if adapter.isTimeoutError(err) {
			return storeadapter.ErrorTimeout
		}

		if err != nil {
			if finalErr == nil {
				finalErr = storeadapter.ErrorKeyNotFound
			}
			continue
		}

		if !exists {
			if finalErr == nil {
				finalErr = storeadapter.ErrorKeyNotFound
			}
			continue
		}

		if stat.NumChildren > 0 {
			nodeKeys, _, err := adapter.client.Children(key)

			if err != nil {
				if finalErr == nil {
					finalErr = err
				}
				continue
			}

			for _, child := range nodeKeys {
				err := adapter.Delete(adapter.combineKeys(key, child))
				if err != nil {
					if finalErr == nil {
						finalErr = err
					}
					continue
				}
			}
		}

		err = adapter.client.Delete(key, -1)
		if finalErr == nil {
			finalErr = err
		}
	}

	return finalErr
}

func (adapter *ZookeeperStoreAdapter) Watch(key string) (<-chan storeadapter.WatchEvent, chan<- bool, <-chan error) {
	panic("not implemented, probably never will be")
	return nil, nil, nil
}

func (adapter *ZookeeperStoreAdapter) Create(node storeadapter.StoreNode) error {
	panic("not implemented")
	return nil
}

func (adapter *ZookeeperStoreAdapter) UpdateDirTTL(key string, ttl uint64) error {
	panic("not implemented")
	return nil
}

func (adapter *ZookeeperStoreAdapter) Update(node storeadapter.StoreNode) error {
	panic("not implemented")
	return nil
}

func (adapter *ZookeeperStoreAdapter) isMissingKeyError(err error) bool {
	return err == zk.ErrNoNode
}

func (adapter *ZookeeperStoreAdapter) isTimeoutError(err error) bool {
	return err == zk.ErrConnectionClosed
}

func (adapter *ZookeeperStoreAdapter) encode(data []byte, TTL uint64, updateTime time.Time) []byte {
	return []byte(fmt.Sprintf("%d,%d,%s", updateTime.Unix(), TTL, string(data)))
}

func (adapter *ZookeeperStoreAdapter) decode(input []byte) (data []byte, TTL uint64, updateTime time.Time, err error) {
	arr := strings.SplitN(string(input), ",", 3)
	if len(arr) != 3 {
		return []byte{}, 0, time.Time{}, fmt.Errorf("Expected an encoded string of the form updateTime,TTL,data got %s", string(input))
	}
	updateTimeInSeconds, err := strconv.ParseInt(arr[0], 10, 64)
	if err != nil {
		return []byte{}, 0, time.Time{}, err
	}
	TTL, err = strconv.ParseUint(arr[1], 10, 64)
	if err != nil {
		return []byte{}, 0, time.Time{}, err
	}
	return []byte(arr[2]), TTL, time.Unix(updateTimeInSeconds, 0), err
}

func (adapter *ZookeeperStoreAdapter) getWithTTLPolicy(key string) fetchedNode {
	data, _, err := adapter.client.Get(key)

	if adapter.isTimeoutError(err) {
		return fetchedNode{err: storeadapter.ErrorTimeout}
	}

	if adapter.isMissingKeyError(err) {
		return fetchedNode{err: storeadapter.ErrorKeyNotFound}
	}

	if err != nil {
		return fetchedNode{err: err}
	}

	if len(data) == 0 {
		return fetchedNode{node: storeadapter.StoreNode{
			Key:   key,
			Value: data,
			Dir:   true,
		}}
	}

	value, TTL, updateTime, err := adapter.decode(data)
	if err != nil {
		return fetchedNode{err: storeadapter.ErrorInvalidFormat}
	}

	if TTL > 0 {
		elapsedTime := int64(math.Floor(adapter.timeProvider.Time().Sub(updateTime).Seconds()))
		remainingTTL := int64(TTL) - elapsedTime
		if remainingTTL > 0 {
			if remainingTTL < int64(TTL) {
				TTL = uint64(remainingTTL)
			}
		} else {
			adapter.client.Delete(key, -1)
			return fetchedNode{isExpired: true}
		}
	}

	return fetchedNode{node: storeadapter.StoreNode{
		Key:   key,
		Value: value,
		TTL:   TTL,
	}}
}

func (adapter *ZookeeperStoreAdapter) createNode(node storeadapter.StoreNode) error {
	root := path.Dir(node.Key)
	var err error
	exists, _, err := adapter.client.Exists(root)
	if err != nil {
		return err
	}
	if !exists {
		err = adapter.createNode(storeadapter.StoreNode{
			Key:   root,
			Value: []byte{},
			TTL:   0,
			Dir:   true,
		})

		if err != nil {
			return err
		}
	}

	if node.Dir {
		_, err = adapter.client.Create(node.Key, []byte{}, 0, zk.WorldACL(zk.PermAll))
	} else {
		_, err = adapter.client.Create(node.Key, adapter.encode(node.Value, node.TTL, adapter.timeProvider.Time()), 0, zk.WorldACL(zk.PermAll))
	}

	if err == zk.ErrNodeExists {
		err = nil
	}

	return err
}

func (adapter *ZookeeperStoreAdapter) getMultipleNodesSimultaneously(rootKey string, nodeKeys []string) (results []storeadapter.StoreNode, err error) {
	fetchedNodes := make(chan fetchedNode, len(nodeKeys))
	for _, nodeKey := range nodeKeys {
		nodeKey := adapter.combineKeys(rootKey, nodeKey)
		adapter.workerPool.ScheduleWork(func() {
			fetchedNodes <- adapter.getWithTTLPolicy(nodeKey)
		})
	}

	numReceived := 0
	for numReceived < len(nodeKeys) {
		fetchedNode := <-fetchedNodes
		numReceived++
		if fetchedNode.isExpired {
			continue
		}
		if fetchedNode.err != nil {
			err = fetchedNode.err
			continue
		}
		results = append(results, fetchedNode.node)
	}

	if err != nil {
		return []storeadapter.StoreNode{}, err
	}

	return results, nil
}

func (adapter *ZookeeperStoreAdapter) pruneZookeepersInternalNodeKeys(keys []string) (prunedNodeKeys []string) {
	for _, key := range keys {
		if key != "zookeeper" {
			prunedNodeKeys = append(prunedNodeKeys, key)
		}
	}
	return prunedNodeKeys
}

func (adapter *ZookeeperStoreAdapter) combineKeys(root string, key string) string {
	if root == "/" {
		return "/" + key
	} else {
		return root + "/" + key
	}
}

func (adapter *ZookeeperStoreAdapter) isNodeDirectory(key string) bool {
	fetchedNode := adapter.getWithTTLPolicy(key)

	if fetchedNode.err != nil {
		return false
	}

	return fetchedNode.node.Dir
}

func (adapter *ZookeeperStoreAdapter) GetAndMaintainLock(lockName string, lockTTL uint64) (lostLock <-chan bool, releaseLock chan<- bool, err error) {
	fmt.Println("NO NO NO NO NO ZooKeeper's store adapter doesn't know how to do locks well!\n")
	releaseLockChannel := make(chan bool, 10)
	return nil, releaseLockChannel, err
}
