package etcdstoreadapter_test

import (
	"fmt"
	. "github.com/cloudfoundry/storeadapter"
	. "github.com/cloudfoundry/storeadapter/etcdstoreadapter"
	"github.com/cloudfoundry/storeadapter/workerpool"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"time"
)

var counter = 0

var _ = Describe("ETCD Store Adapter", func() {
	var (
		adapter       StoreAdapter
		breakfastNode StoreNode
		lunchNode     StoreNode
	)

	BeforeEach(func() {
		breakfastNode = StoreNode{
			Key:   "/menu/breakfast",
			Value: []byte("waffles"),
		}

		lunchNode = StoreNode{
			Key:   "/menu/lunch",
			Value: []byte("burgers"),
		}

		adapter = NewETCDStoreAdapter(etcdRunner.NodeURLS(), workerpool.NewWorkerPool(10))
		err := adapter.Connect()
		Ω(err).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		adapter.Disconnect()
	})

	Describe("Get", func() {
		BeforeEach(func() {
			err := adapter.SetMulti([]StoreNode{breakfastNode, lunchNode})
			Ω(err).ShouldNot(HaveOccurred())
		})

		Context("when getting a key", func() {
			It("should return the appropriate store breakfastNode", func() {
				value, err := adapter.Get("/menu/breakfast")
				Ω(err).ShouldNot(HaveOccurred())
				Ω(value).Should(Equal(breakfastNode))
			})
		})

		Context("When getting a non-existent key", func() {
			It("should return an error", func() {
				value, err := adapter.Get("/not_a_key")
				Ω(err).Should(Equal(ErrorKeyNotFound))
				Ω(value).Should(BeZero())
			})
		})

		Context("when getting a directory", func() {
			It("should return an error", func() {
				value, err := adapter.Get("/menu")
				Ω(err).Should(Equal(ErrorNodeIsDirectory))
				Ω(value).Should(BeZero())
			})
		})

		Context("when the store is down", func() {
			BeforeEach(func() {
				etcdRunner.Stop()
			})

			AfterEach(func() {
				etcdRunner.Start()
			})

			It("should return a timeout error", func() {
				value, err := adapter.Get("/foo/bar")
				Ω(err).Should(Equal(ErrorTimeout))
				Ω(value).Should(BeZero())
			})
		})
	})

	Describe("SetMulti", func() {
		It("should be able to set multiple things to the store at once", func() {
			err := adapter.SetMulti([]StoreNode{breakfastNode, lunchNode})
			Ω(err).ShouldNot(HaveOccurred())

			menu, err := adapter.ListRecursively("/menu")
			Ω(err).ShouldNot(HaveOccurred())
			Ω(menu.ChildNodes).Should(HaveLen(2))
			Ω(menu.ChildNodes).Should(ContainElement(breakfastNode))
			Ω(menu.ChildNodes).Should(ContainElement(lunchNode))
		})

		Context("Setting to an existing node", func() {
			BeforeEach(func() {
				err := adapter.SetMulti([]StoreNode{breakfastNode, lunchNode})
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("should be able to update existing entries", func() {
				lunchNode.Value = []byte("steak")
				err := adapter.SetMulti([]StoreNode{breakfastNode, lunchNode})
				Ω(err).ShouldNot(HaveOccurred())

				menu, err := adapter.ListRecursively("/menu")
				Ω(err).ShouldNot(HaveOccurred())
				Ω(menu.ChildNodes).Should(HaveLen(2))
				Ω(menu.ChildNodes).Should(ContainElement(breakfastNode))
				Ω(menu.ChildNodes).Should(ContainElement(lunchNode))
			})

			It("should error when attempting to set to a directory", func() {
				dirNode := StoreNode{
					Key:   "/menu",
					Value: []byte("oops!"),
				}

				err := adapter.SetMulti([]StoreNode{dirNode})
				Ω(err).Should(Equal(ErrorNodeIsDirectory))
			})
		})

		Context("when the store is down", func() {
			BeforeEach(func() {
				etcdRunner.Stop()
			})

			AfterEach(func() {
				etcdRunner.Start()
			})

			It("should return a timeout error", func() {
				err := adapter.SetMulti([]StoreNode{breakfastNode})
				Ω(err).Should(Equal(ErrorTimeout))
			})
		})
	})

	Describe("List", func() {
		BeforeEach(func() {
			err := adapter.SetMulti([]StoreNode{breakfastNode, lunchNode})
			Ω(err).ShouldNot(HaveOccurred())
		})

		Context("When listing a directory", func() {
			It("Should list directory contents", func() {
				value, err := adapter.ListRecursively("/menu")
				Ω(err).ShouldNot(HaveOccurred())
				Ω(value.Key).Should(Equal("/menu"))
				Ω(value.Dir).Should(BeTrue())
				Ω(value.ChildNodes).Should(HaveLen(2))
				Ω(value.ChildNodes).Should(ContainElement(breakfastNode))
				Ω(value.ChildNodes).Should(ContainElement(lunchNode))
			})
		})

		Context("when listing a directory that contains directories", func() {
			var (
				firstCourseDinnerNode  StoreNode
				secondCourseDinnerNode StoreNode
			)

			BeforeEach(func() {
				firstCourseDinnerNode = StoreNode{
					Key:   "/menu/dinner/first_course",
					Value: []byte("Salad"),
				}
				secondCourseDinnerNode = StoreNode{
					Key:   "/menu/dinner/second_course",
					Value: []byte("Brisket"),
				}
				err := adapter.SetMulti([]StoreNode{firstCourseDinnerNode, secondCourseDinnerNode})

				Ω(err).ShouldNot(HaveOccurred())
			})

			Context("when listing the root directory", func() {
				It("should list the contents recursively", func() {
					value, err := adapter.ListRecursively("/")
					Ω(err).ShouldNot(HaveOccurred())
					Ω(value.Key).Should(Equal("/"))
					Ω(value.Dir).Should(BeTrue())
					Ω(value.ChildNodes).Should(HaveLen(1))
					menuNode := value.ChildNodes[0]
					Ω(menuNode.Key).Should(Equal("/menu"))
					Ω(menuNode.Value).Should(BeEmpty())
					Ω(menuNode.Dir).Should(BeTrue())
					Ω(menuNode.ChildNodes).Should(HaveLen(3))
					Ω(menuNode.ChildNodes).Should(ContainElement(breakfastNode))
					Ω(menuNode.ChildNodes).Should(ContainElement(lunchNode))

					var dinnerNode StoreNode
					for _, node := range menuNode.ChildNodes {
						if node.Key == "/menu/dinner" {
							dinnerNode = node
							break
						}
					}
					Ω(dinnerNode.Dir).Should(BeTrue())
					Ω(dinnerNode.ChildNodes).Should(ContainElement(firstCourseDinnerNode))
					Ω(dinnerNode.ChildNodes).Should(ContainElement(secondCourseDinnerNode))
				})
			})

			Context("when listing another directory", func() {
				It("should list the contents recursively", func() {
					menuNode, err := adapter.ListRecursively("/menu")
					Ω(err).ShouldNot(HaveOccurred())
					Ω(menuNode.Key).Should(Equal("/menu"))
					Ω(menuNode.Value).Should(BeEmpty())
					Ω(menuNode.Dir).Should(BeTrue())
					Ω(menuNode.ChildNodes).Should(HaveLen(3))
					Ω(menuNode.ChildNodes).Should(ContainElement(breakfastNode))
					Ω(menuNode.ChildNodes).Should(ContainElement(lunchNode))

					var dinnerNode StoreNode
					for _, node := range menuNode.ChildNodes {
						if node.Key == "/menu/dinner" {
							dinnerNode = node
							break
						}
					}
					Ω(dinnerNode.Dir).Should(BeTrue())
					Ω(dinnerNode.ChildNodes).Should(ContainElement(firstCourseDinnerNode))
					Ω(dinnerNode.ChildNodes).Should(ContainElement(secondCourseDinnerNode))
				})
			})
		})

		Context("when listing an empty directory", func() {
			It("should return an empty list of breakfastNodes, and not error", func() {
				err := adapter.SetMulti([]StoreNode{
					{
						Key:   "/empty_dir/temp",
						Value: []byte("foo"),
					},
				})
				Ω(err).ShouldNot(HaveOccurred())

				err = adapter.Delete("/empty_dir/temp")
				Ω(err).ShouldNot(HaveOccurred())

				value, err := adapter.ListRecursively("/empty_dir")
				Ω(err).ShouldNot(HaveOccurred())
				Ω(value.Key).Should(Equal("/empty_dir"))
				Ω(value.Dir).Should(BeTrue())
				Ω(value.ChildNodes).Should(HaveLen(0))
			})
		})

		Context("when listing a non-existent key", func() {
			It("should return an error", func() {
				value, err := adapter.ListRecursively("/nothing-here")
				Ω(err).Should(Equal(ErrorKeyNotFound))
				Ω(value).Should(BeZero())
			})
		})

		Context("when listing an entry", func() {
			It("should return an error", func() {
				value, err := adapter.ListRecursively("/menu/breakfast")
				Ω(err).Should(HaveOccurred())
				Ω(err).Should(Equal(ErrorNodeIsNotDirectory))
				Ω(value).Should(BeZero())
			})
		})

		Context("when the store is down", func() {
			BeforeEach(func() {
				etcdRunner.Stop()
			})

			AfterEach(func() {
				etcdRunner.Start()
			})

			It("should return a timeout error", func() {
				value, err := adapter.ListRecursively("/menu")
				Ω(err).Should(Equal(ErrorTimeout))
				Ω(value).Should(BeZero())
			})
		})
	})

	Describe("Delete", func() {
		BeforeEach(func() {
			err := adapter.SetMulti([]StoreNode{breakfastNode, lunchNode})
			Ω(err).ShouldNot(HaveOccurred())
		})

		Context("when deleting existing keys", func() {
			It("should delete the keys", func() {
				err := adapter.Delete("/menu/breakfast", "/menu/lunch")
				Ω(err).ShouldNot(HaveOccurred())

				value, err := adapter.Get("/menu/breakfast")
				Ω(err).Should(Equal(ErrorKeyNotFound))
				Ω(value).Should(BeZero())

				value, err = adapter.Get("/menu/lunch")
				Ω(err).Should(Equal(ErrorKeyNotFound))
				Ω(value).Should(BeZero())
			})
		})

		Context("when deleting a non-existing key", func() {
			It("should error", func() {
				err := adapter.Delete("/not-a-key")
				Ω(err).Should(Equal(ErrorKeyNotFound))
			})
		})

		Context("when deleting a directory", func() {
			It("deletes the key and its contents", func() {
				err := adapter.Delete("/menu")
				Ω(err).ShouldNot(HaveOccurred())

				_, err = adapter.Get("/menu/breakfast")
				Ω(err).Should(Equal(ErrorKeyNotFound))

				_, err = adapter.Get("/menu")
				Ω(err).Should(Equal(ErrorKeyNotFound))
			})
		})

		Context("when the store is down", func() {
			BeforeEach(func() {
				etcdRunner.Stop()
			})

			AfterEach(func() {
				etcdRunner.Start()
			})

			It("should return a timeout error", func() {
				err := adapter.Delete("/menu/breakfast")
				Ω(err).Should(Equal(ErrorTimeout))
			})
		})
	})

	Context("When setting a key with a non-zero TTL", func() {
		It("should stay in the store for the duration of its TTL and then disappear", func() {
			breakfastNode.TTL = 1
			err := adapter.SetMulti([]StoreNode{breakfastNode})
			Ω(err).ShouldNot(HaveOccurred())

			_, err = adapter.Get("/menu/breakfast")
			Ω(err).ShouldNot(HaveOccurred())

			Eventually(func() interface{} {
				_, err = adapter.Get("/menu/breakfast")
				return err
			}, 2, 0.01).Should(Equal(ErrorKeyNotFound)) // as of etcd v0.2rc1, etcd seems to take an extra 0.5 seconds to expire its TTLs
		})
	})

	Describe("Maintaining a node's presence (and lack thereof)", func() {
		var (
			uniqueStoreNodeForThisTest StoreNode //avoid collisions between test runs
		)

		BeforeEach(func() {
			uniqueStoreNodeForThisTest = StoreNode{
				Key: fmt.Sprintf("analyzer-%d", counter),
				TTL: 1,
			}

			counter++
		})

		Context("when passed a TTL of 0", func() {
			It("should be like, no way man", func() {
				uniqueStoreNodeForThisTest.TTL = 0

				lostLock, releaseLock, err := adapter.MaintainNode(uniqueStoreNodeForThisTest)
				Ω(err).Should(Equal(ErrorInvalidTTL))
				Ω(lostLock).Should(BeNil())
				Ω(releaseLock).Should(BeNil())
			})
		})

		Context("when the store is not available", func() {
			BeforeEach(func() {
				etcdRunner.Stop()
			})

			AfterEach(func() {
				etcdRunner.Start()
			})

			It("returns an error", func() {
				lastLock, releaseLock, err := adapter.MaintainNode(uniqueStoreNodeForThisTest)
				Ω(err).Should(Equal(ErrorTimeout))
				Ω(lastLock).Should(BeNil())
				Ω(releaseLock).Should(BeNil())
			})
		})

		Context("when the lock is available", func() {
			It("should return immediately", func(done Done) {
				lostLock, releaseLock, err := adapter.MaintainNode(uniqueStoreNodeForThisTest)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(lostLock).ShouldNot(BeNil())
				Ω(releaseLock).ShouldNot(BeNil())
				close(done)
			}, 1.0)

			It("should maintain the lock in the background", func(done Done) {
				adapter.MaintainNode(uniqueStoreNodeForThisTest)

				secondLockingCallDidGrabLock := false
				go func() {
					adapter.MaintainNode(uniqueStoreNodeForThisTest)
					secondLockingCallDidGrabLock = true
				}()

				time.Sleep(3 * time.Second)

				Ω(secondLockingCallDidGrabLock).Should(BeFalse())

				close(done)
			}, 10.0)

			Context("when a value is given", func() {
				BeforeEach(func() {
					uniqueStoreNodeForThisTest.Value = []byte("some value")
				})

				It("creates the lock with the given value", func(done Done) {
					_, _, err := adapter.MaintainNode(uniqueStoreNodeForThisTest)
					Ω(err).ShouldNot(HaveOccurred())

					val, err := adapter.Get(uniqueStoreNodeForThisTest.Key)
					Ω(err).ShouldNot(HaveOccurred())

					Ω(string(val.Value)).Should(Equal("some value"))
					close(done)
				}, 1.0)
			})

			Context("when a value is NOT given", func() {
				It("creates the lock with some unique value", func(done Done) {
					otherUniqueStoreNodeForThisTest := uniqueStoreNodeForThisTest
					otherUniqueStoreNodeForThisTest.Key = otherUniqueStoreNodeForThisTest.Key + "other"

					_, _, err := adapter.MaintainNode(uniqueStoreNodeForThisTest)
					Ω(err).ShouldNot(HaveOccurred())

					_, _, err = adapter.MaintainNode(otherUniqueStoreNodeForThisTest)
					Ω(err).ShouldNot(HaveOccurred())

					val, err := adapter.Get(uniqueStoreNodeForThisTest.Key)
					Ω(err).ShouldNot(HaveOccurred())

					otherval, err := adapter.Get(otherUniqueStoreNodeForThisTest.Key)
					Ω(err).ShouldNot(HaveOccurred())

					Ω(string(val.Value)).ShouldNot(Equal(string(otherval.Value)))
					close(done)
				}, 1.0)
			})

			Context("when the lock disappears after it has been acquired (e.g. ETCD store is reset)", func() {
				AfterEach(func() {
					etcdRunner.Start()
				})

				It("should send a notification down the lostLockChannel", func(done Done) {
					lostLock, _, _ := adapter.MaintainNode(uniqueStoreNodeForThisTest)

					etcdRunner.Stop()

					Ω(<-lostLock).Should(BeTrue())

					close(done)
				}, 1.0)
			})
		})

		Context("when releasing the lock", func() {
			It("makes it available for others trying to acquire it", func(done Done) {
				defer close(done)

				_, releaseLock, err := adapter.MaintainNode(uniqueStoreNodeForThisTest)
				Ω(err).ShouldNot(HaveOccurred())

				gotLock := make(chan bool)
				go func() {
					_, _, err := adapter.MaintainNode(uniqueStoreNodeForThisTest)
					Ω(err).ShouldNot(HaveOccurred())

					gotLock <- true
				}()

				Consistently(gotLock).ShouldNot(Receive())

				releasedLock := make(chan bool)
				releaseLock <- releasedLock
				Eventually(releasedLock).Should(BeClosed())

				Eventually(gotLock, 2.0).Should(Receive())
			}, 5.0)

			It("deletes the lock's key", func(done Done) {
				defer close(done)

				_, releaseLock, err := adapter.MaintainNode(uniqueStoreNodeForThisTest)
				Ω(err).ShouldNot(HaveOccurred())

				_, err = adapter.Get(uniqueStoreNodeForThisTest.Key)
				Ω(err).ShouldNot(HaveOccurred())

				releasedLock := make(chan bool)
				releaseLock <- releasedLock
				Eventually(releasedLock).Should(BeClosed())

				_, err = adapter.Get(uniqueStoreNodeForThisTest.Key)
				Ω(err).Should(HaveOccurred())
			}, 5.0)
		})
	})

	Describe("Creating", func() {
		var node StoreNode

		BeforeEach(func() {
			node = StoreNode{Key: "/foo", Value: []byte("some value")}
			err := adapter.Create(node)
			Ω(err).ShouldNot(HaveOccurred())
		})

		It("creates the node at the given key", func() {
			retrievedNode, err := adapter.Get("/foo")
			Ω(err).ShouldNot(HaveOccurred())
			Ω(retrievedNode).Should(Equal(node))
		})

		Context("when a node already exists at the key", func() {
			It("returns an error", func() {
				err := adapter.Create(node)
				Ω(err).Should(Equal(ErrorKeyExists))
			})
		})

		Context("when a directory exists at the given key", func() {
			It("returns an error", func() {
				err := adapter.Create(StoreNode{Key: "/dir/foo", Value: []byte("some value")})
				Ω(err).ShouldNot(HaveOccurred())

				err = adapter.Create(StoreNode{Key: "/dir", Value: []byte("some value")})
				Ω(err).Should(Equal(ErrorKeyExists))
			})
		})
	})

	Describe("Updating", func() {
		var node StoreNode

		BeforeEach(func() {
			node = StoreNode{Key: "/foo", Value: []byte("some value")}
		})

		It("updates the existing node at the given key", func() {
			err := adapter.Create(node)
			Ω(err).ShouldNot(HaveOccurred())

			node.Value = []byte("some new value")

			err = adapter.Update(node)
			Ω(err).ShouldNot(HaveOccurred())

			retrievedNode, err := adapter.Get("/foo")
			Ω(err).ShouldNot(HaveOccurred())
			Ω(retrievedNode).Should(Equal(node))
		})

		Context("when a node does not exist at the key", func() {
			It("returns an error", func() {
				err := adapter.Update(node)
				Ω(err).Should(Equal(ErrorKeyNotFound))
			})
		})

		Context("when a directory exists at the given key", func() {
			It("returns an error", func() {
				err := adapter.Create(StoreNode{Key: "/dir/foo", Value: []byte("some value")})
				Ω(err).ShouldNot(HaveOccurred())

				err = adapter.Update(StoreNode{Key: "/dir", Value: []byte("some value")})
				Ω(err).Should(Equal(ErrorNodeIsDirectory))
			})
		})
	})

	Describe("Comparing as well as swapping", func() {
		var node StoreNode

		BeforeEach(func() {
			node = StoreNode{Key: "/foo", Value: []byte("some value")}
		})

		It("updates the existing node at the given key", func() {
			err := adapter.Create(node)
			Ω(err).ShouldNot(HaveOccurred())

			newNode := node
			newNode.Value = []byte("some new value")

			err = adapter.CompareAndSwap(node, newNode)
			Ω(err).ShouldNot(HaveOccurred())

			retrievedNode, err := adapter.Get("/foo")
			Ω(err).ShouldNot(HaveOccurred())
			Ω(retrievedNode).Should(Equal(newNode))
		})

		Context("when a node exists but the comparison fails", func() {
			It("returns an error", func() {
				err := adapter.Create(node)
				Ω(err).ShouldNot(HaveOccurred())

				wrongNode := node
				wrongNode.Value = []byte("NOPE")

				newNode := node
				newNode.Value = []byte("some new value")

				err = adapter.CompareAndSwap(wrongNode, newNode)
				Ω(err).Should(Equal(ErrorKeyComparisonFailed))

				retrievedNode, err := adapter.Get("/foo")
				Ω(err).ShouldNot(HaveOccurred())
				Ω(retrievedNode).Should(Equal(node))
			})
		})

		Context("when a node does not exist at the key", func() {
			It("returns an error", func() {
				err := adapter.CompareAndSwap(node, node)
				Ω(err).Should(Equal(ErrorKeyNotFound))
			})
		})

		Context("when a directory exists at the given key", func() {
			It("returns an error", func() {
				err := adapter.Create(StoreNode{Key: "/dir/foo", Value: []byte("some value")})
				Ω(err).ShouldNot(HaveOccurred())

				newNode := StoreNode{Key: "/dir", Value: []byte("some value")}

				err = adapter.CompareAndSwap(newNode, newNode)
				Ω(err).Should(Equal(ErrorNodeIsDirectory))
			})
		})
	})

	Describe("Watching", func() {
		Context("when a node under the key is created", func() {
			It("sends an event with CreateEvent type and the node's value", func(done Done) {
				events, _, _ := adapter.Watch("/foo")

				err := adapter.Create(StoreNode{
					Key:   "/foo/a",
					Value: []byte("new value"),
				})
				Expect(err).ToNot(HaveOccurred())

				event := <-events
				Expect(event.Type).To(Equal(CreateEvent))
				Expect(event.Node.Key).To(Equal("/foo/a"))
				Expect(string(event.Node.Value)).To(Equal("new value"))

				close(done)
			}, 5.0)
		})

		Context("when a node under the key is set", func() {
			BeforeEach(func() {
				err := adapter.SetMulti([]StoreNode{
					{
						Key:   "/foo/a",
						Value: []byte("some value"),
					},
				})
				Expect(err).ToNot(HaveOccurred())
			})

			It("sends an event with UpdateEvent type and the node's value", func(done Done) {
				events, _, _ := adapter.Watch("/foo")

				err := adapter.SetMulti([]StoreNode{
					{
						Key:   "/foo/a",
						Value: []byte("new value"),
					},
				})
				Expect(err).ToNot(HaveOccurred())

				event := <-events
				Expect(event.Type).To(Equal(UpdateEvent))
				Expect(event.Node.Key).To(Equal("/foo/a"))
				Expect(string(event.Node.Value)).To(Equal("new value"))

				close(done)
			}, 5.0)
		})

		Context("when a node under the key is updated", func() {
			BeforeEach(func() {
				err := adapter.SetMulti([]StoreNode{
					{
						Key:   "/foo/a",
						Value: []byte("some value"),
					},
				})
				Expect(err).ToNot(HaveOccurred())
			})

			It("sends an event with UpdateEvent type and the node's value", func(done Done) {
				events, _, _ := adapter.Watch("/foo")

				err := adapter.UpdateDirTTL("/foo", 10)
				Expect(err).ToNot(HaveOccurred())

				event := <-events
				Expect(event.Type).To(Equal(UpdateEvent))
				Expect(event.Node.Key).To(Equal("/foo"))
				Expect(event.Node.TTL).To(BeNumerically("==", 10))

				close(done)
			}, 5.0)
		})

		Context("when a node under the key is deleted", func() {
			BeforeEach(func() {
				err := adapter.SetMulti([]StoreNode{
					{
						Key:   "/foo/a",
						Value: []byte("some value"),
					},
				})
				Expect(err).ToNot(HaveOccurred())
			})

			It("sends an event with DeleteEvent type and the node's value", func(done Done) {
				events, _, _ := adapter.Watch("/foo")

				err := adapter.Delete("/foo/a")
				Expect(err).ToNot(HaveOccurred())

				event := <-events
				Expect(event.Type).To(Equal(DeleteEvent))
				Expect(event.Node.Key).To(Equal("/foo/a"))
				Expect(string(event.Node.Value)).To(Equal("some value"))

				close(done)
			}, 5.0)
		})

		Context("when a node under the key expires", func() {
			BeforeEach(func() {
				err := adapter.SetMulti([]StoreNode{
					{
						Key:   "/foo/a",
						Value: []byte("some value"),
						TTL:   1,
					},
				})
				Expect(err).ToNot(HaveOccurred())
			})

			It("sends an event with ExpireEvent type and the node's value", func(done Done) {
				events, _, _ := adapter.Watch("/foo")

				time.Sleep(2 * time.Second)

				event := <-events
				Expect(event.Type).To(Equal(ExpireEvent))
				Expect(event.Node.Key).To(Equal("/foo/a"))
				Expect(string(event.Node.Value)).To(Equal("some value"))

				close(done)
			}, 5.0)
		})

		Context("when told to stop watching", func() {
			It("no longer notifies for any events", func(done Done) {
				events, stop, errors := adapter.Watch("/foo")

				err := adapter.Create(StoreNode{
					Key:   "/foo/a",
					Value: []byte("new value"),
				})
				Expect(err).ToNot(HaveOccurred())

				event := <-events
				Expect(event.Type).To(Equal(CreateEvent))
				Expect(event.Node.Key).To(Equal("/foo/a"))
				Expect(string(event.Node.Value)).To(Equal("new value"))

				stop <- true

				err = adapter.SetMulti([]StoreNode{
					{
						Key:   "/foo/b",
						Value: []byte("new value"),
					},
				})
				Expect(err).ToNot(HaveOccurred())

				Expect(events).To(BeClosed())
				Expect(errors).To(BeClosed())

				close(done)
			}, 5.0)
		})

		Context("when told to disconnect", func() {
			It("no longer notifies for any events", func() {
				events, _, errors := adapter.Watch("/foo")

				adapter.Disconnect()

				Eventually(events).Should(BeClosed())
				Eventually(errors).Should(BeClosed())
			})
		})

		Context("when 1000 (current etcd constant) events occur between the start index and now", func() {
			It("skips the missing event history and eventually catches up", func() {
				events, _, errChan := adapter.Watch("/foo")

				err := adapter.Create(StoreNode{
					Key:   "/foo/a",
					Value: []byte("new value"),
				})
				Expect(err).ToNot(HaveOccurred())

				event := <-events
				Expect(event.Type).To(Equal(CreateEvent))
				Expect(event.Node.Key).To(Equal("/foo/a"))
				Expect(string(event.Node.Value)).To(Equal("new value"))

				for i, _ := range make([]bool, 1003) {
					err := adapter.SetMulti([]StoreNode{
						{
							Key:   "/foo/a",
							Value: []byte(fmt.Sprintf("%d", i+1)),
						},
					})
					Expect(err).ToNot(HaveOccurred())
				}

				event = <-events
				Expect(event.Type).To(Equal(UpdateEvent))
				Expect(event.Node.Key).To(Equal("/foo/a"))
				Expect(string(event.Node.Value)).To(Equal("1"))

				// 2 and 3 will be missed (rolling window of 1000)

				event = <-events
				Expect(event.Type).To(Equal(UpdateEvent))
				Expect(event.Node.Key).To(Equal("/foo/a"))
				Expect(string(event.Node.Value)).To(Equal("4"))

				Expect(errChan).To(BeEmpty())
			})
		})

		Context("when etcd disappears mid-watch", func() {
			AfterEach(func() {
				etcdRunner.Start()
			})

			It("should write to the error channel", func(done Done) {
				events, _, errChan := adapter.Watch("/foo")

				err := adapter.Create(StoreNode{
					Key:   "/foo/a",
					Value: []byte("new value"),
				})
				Expect(err).ToNot(HaveOccurred())

				etcdRunner.Stop()

				event := <-events
				Expect(event.Type).To(Equal(CreateEvent))
				Expect(event.Node.Key).To(Equal("/foo/a"))

				Ω(<-errChan).Should(Equal(ErrorTimeout))

				close(done)
			}, 5)
		})
	})

	Describe("UpdateDirTTL", func() {
		Context("When the directory exists", func() {
			It("should set the TTL", func() {
				err := adapter.Create(breakfastNode)
				Expect(err).NotTo(HaveOccurred())

				err = adapter.UpdateDirTTL("/menu", 1)
				Expect(err).NotTo(HaveOccurred())

				node, err := adapter.ListRecursively("/menu")
				Expect(err).NotTo(HaveOccurred())
				Expect(node.TTL).NotTo(BeZero())

				_, err = adapter.Get("/menu/breakfast")
				Expect(err).NotTo(HaveOccurred())

				time.Sleep(2 * time.Second)

				_, err = adapter.Get("/menu/breakfast")
				Expect(err).To(Equal(ErrorKeyNotFound))
			})
		})

		Context("When the directory does not exist", func() {
			It("should return a ErrorKeyNotFound", func() {
				err := adapter.UpdateDirTTL("/non-existent-key", 1)
				Expect(err).To(Equal(ErrorKeyNotFound))
			})
		})

		Context("When the key represents a leaf, not a directory", func() {
			It("should return a ErrorNodeIsNotDirectory error", func() {
				err := adapter.Create(breakfastNode)
				Expect(err).NotTo(HaveOccurred())

				err = adapter.UpdateDirTTL("/menu/breakfast", 1)
				Expect(err).To(Equal(ErrorNodeIsNotDirectory))
			})
		})
	})
})
