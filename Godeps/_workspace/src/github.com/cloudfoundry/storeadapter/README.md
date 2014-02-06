storeadapter
============

[![Build Status](https://travis-ci.org/cloudfoundry/storeadapter.png)](https://travis-ci.org/cloudfoundry/storeadapter)

Golang interface for ETCD/ZooKeeper style datastores

### `storeadapter`

The `storeadapter` is an generalized client for connecting to a Zookeeper/ETCD-like high availability store.  Writes are performed concurrently for optimal performance.


#### `fakestoreadapter`

Provides a fake in-memory implementation of the `storeadapter` to allow for unit tests that do not need to spin up a database.

#### `workerpool`

Provides a worker pool with a configurable pool size.  Work scheduled on the pool will run concurrently, but no more `poolSize` workers can be running at any given moment.

#### `storerunner`

Brings up and manages the lifecycle of a live ETCD/ZooKeeper server cluster.
