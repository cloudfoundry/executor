package shared_test

import (
	. "github.com/cloudfoundry-incubator/runtime-schema/bbs/shared"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/storeadapter"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Shared", func() {
	Describe("#RetryIndefinitelyOnStoreTimeout", func() {
		Context("when the callback returns a storeadapter.ErrorTimeout", func() {
			It("should call the callback again", func() {
				numCalls := 0

				callback := func() error {
					numCalls++
					if numCalls == 1 {
						return storeadapter.ErrorTimeout
					}
					return nil
				}

				RetryIndefinitelyOnStoreTimeout(callback)
				Ω(numCalls).Should(Equal(2))
			})
		})

		Context("when the callback does not return a storeadapter.ErrorTimeout", func() {
			It("should call the callback once", func() {
				numCalls := 0

				callback := func() error {
					numCalls++
					return nil
				}

				RetryIndefinitelyOnStoreTimeout(callback)
				Ω(numCalls).Should(Equal(1))
			})
		})
	})

	Describe("WatchWithFilter", func() {
		It("panics if it receives bad arguments", func() {
			Ω(func() { WatchWithFilter(etcdClient, "", nil, "") }).Should(Panic())

			Ω(func() { WatchWithFilter(etcdClient, "", nil, func() {}) }).Should(Panic())
			Ω(func() { WatchWithFilter(etcdClient, "", nil, func(int) {}) }).Should(Panic())
			Ω(func() { WatchWithFilter(etcdClient, "", nil, func(storeadapter.WatchEvent) {}) }).Should(Panic())
			Ω(func() { WatchWithFilter(etcdClient, "", nil, func(storeadapter.WatchEvent) (int, int) { return 0, 0 }) }).Should(Panic())
			Ω(func() {
				WatchWithFilter(etcdClient, "", make(chan<- struct{}), func(storeadapter.WatchEvent) (int, bool) { return 0, true })
			}).Should(Panic())
			Ω(func() {
				WatchWithFilter(etcdClient, "", make(chan models.StopLRPInstance), func(storeadapter.WatchEvent) (models.StopLRPInstance, bool) { return models.StopLRPInstance{}, true })
			}).ShouldNot(Panic())
		})

		Describe("watching for events", func() {
			var (
				events  chan storeadapter.WatchEvent
				stop    chan<- bool
				errors  <-chan error
				stopped bool
			)

			BeforeEach(func() {
				events = make(chan storeadapter.WatchEvent)
				filter := func(event storeadapter.WatchEvent) (storeadapter.WatchEvent, bool) {
					if event.Node != nil && event.Node.Key == "/ignore" {
						return event, false
					}
					return event, true
				}
				stop, errors = WatchWithFilter(etcdClient, "/", events, filter)
			})

			AfterEach(func() {
				if !stopped {
					stop <- true
				}
			})

			It("sends an event down the pipe for creates", func() {
				err := etcdClient.Create(storeadapter.StoreNode{
					Key:   "/foo",
					Value: []byte("bar"),
				})
				Ω(err).ShouldNot(HaveOccurred())

				var receivedEvent storeadapter.WatchEvent
				Eventually(events).Should(Receive(&receivedEvent))
				Ω(receivedEvent.Node.Value).Should(Equal([]byte("bar")))
			})

			It("sends an event down the pipe for updates", func() {
				err := etcdClient.Create(storeadapter.StoreNode{
					Key:   "/foo",
					Value: []byte("bar"),
				})
				Ω(err).ShouldNot(HaveOccurred())

				Eventually(events).Should(Receive())

				err = etcdClient.SetMulti([]storeadapter.StoreNode{
					{
						Key:   "/foo",
						Value: []byte("bar-updated"),
					},
				})
				Ω(err).ShouldNot(HaveOccurred())

				var receivedEvent storeadapter.WatchEvent
				Eventually(events).Should(Receive(&receivedEvent))
				Ω(receivedEvent.PrevNode.Value).Should(Equal([]byte("bar")))
				Ω(receivedEvent.Node.Value).Should(Equal([]byte("bar-updated")))

			})

			It("sends an event down the pipe for deletes", func() {
				err := etcdClient.Create(storeadapter.StoreNode{
					Key:   "/foo",
					Value: []byte("bar"),
				})
				Ω(err).ShouldNot(HaveOccurred())

				Eventually(events).Should(Receive())

				err = etcdClient.Delete("/foo")
				Ω(err).ShouldNot(HaveOccurred())

				var receivedEvent storeadapter.WatchEvent
				Eventually(events).Should(Receive(&receivedEvent))
				Ω(receivedEvent.PrevNode.Value).Should(Equal([]byte("bar")))
				Ω(receivedEvent.Node).Should(BeNil())
			})

			Context("when the filter says no", func() {
				It("should not return an event", func() {
					err := etcdClient.Create(storeadapter.StoreNode{
						Key:   "/ignore",
						Value: []byte("bar"),
					})
					Ω(err).ShouldNot(HaveOccurred())

					Consistently(events).ShouldNot(Receive())
				})
			})

			It("closes the events and errors channel when told to stop", func() {
				stop <- true
				stopped = true

				Eventually(events).Should(BeClosed())
				Eventually(errors).Should(BeClosed())
			})
		})
	})
})
