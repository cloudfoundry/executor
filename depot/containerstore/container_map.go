package containerstore

import (
	"sync"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/garden"
)

type storeNode struct {
	modifiedIndex uint
	executor.Container
	GardenContainer garden.Container
}

func newStoreNode(container executor.Container) storeNode {
	return storeNode{
		Container:     container,
		modifiedIndex: 0,
	}
}

type nodeMap struct {
	nodes map[string]storeNode
	lock  *sync.RWMutex
}

func newNodeMap() nodeMap {
	return nodeMap{
		nodes: make(map[string]storeNode),
		lock:  &sync.RWMutex{},
	}
}

func (n nodeMap) Contains(guid string) bool {
	n.lock.RLock()
	defer n.lock.RUnlock()
	_, ok := n.nodes[guid]
	return ok
}

func (n nodeMap) Add(node storeNode) error {
	n.lock.Lock()
	defer n.lock.Unlock()

	if _, ok := n.nodes[node.Guid]; ok {
		return executor.ErrContainerGuidNotAvailable
	}

	n.nodes[node.Guid] = node

	return nil
}

func (n nodeMap) Remove(guid string) {
	n.lock.Lock()
	defer n.lock.Unlock()

	delete(n.nodes, guid)
}

func (n nodeMap) Get(guid string) (storeNode, error) {
	n.lock.RLock()
	defer n.lock.RUnlock()

	node, ok := n.nodes[guid]
	if !ok {
		return storeNode{}, executor.ErrContainerNotFound
	}

	return node, nil
}

func (n nodeMap) List() []storeNode {
	n.lock.RLock()
	defer n.lock.RUnlock()

	list := make([]storeNode, 0, len(n.nodes))
	for _, node := range n.nodes {
		list = append(list, node)
	}
	return list
}

func (n nodeMap) CAS(node storeNode) (storeNode, error) {
	n.lock.Lock()
	defer n.lock.Unlock()

	existingNode, ok := n.nodes[node.Guid]
	if !ok {
		return storeNode{}, executor.ErrContainerNotFound
	}

	if existingNode.modifiedIndex != node.modifiedIndex {
		return storeNode{}, ErrFailedToCAS
	}

	node.modifiedIndex++
	n.nodes[node.Guid] = node

	return node, nil
}

func (n nodeMap) CAD(node storeNode) error {
	n.lock.Lock()
	defer n.lock.Unlock()

	existingNode, ok := n.nodes[node.Guid]
	if !ok {
		return executor.ErrContainerNotFound
	}

	if existingNode.modifiedIndex != node.modifiedIndex {
		return ErrFailedToCAS
	}

	delete(n.nodes, node.Guid)

	return nil
}
