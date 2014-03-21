package bbs_test

import (
	. "github.com/cloudfoundry-incubator/runtime-schema/bbs"
	"github.com/cloudfoundry/storeadapter"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Presence", func() {
	var (
		presence Presence
		key      string
		value    string
		interval time.Duration
	)

	BeforeEach(func() {
		key = "/v1/some-key"
		value = "some-value"

		presence = NewPresence(store, key, []byte(value))
		interval = 1 * time.Second
	})

	Describe("Maintain", func() {
		var (
			locked bool
			err    error
		)

		BeforeEach(func() {
			var status <-chan bool
			status, err = presence.Maintain(interval)
			Ω(err).ShouldNot(HaveOccurred())

			locked = false
			var ok bool
			go func() {
				for {
					select {
					case locked, ok = <-status:
						if !ok {
							return
						}
					}
				}
			}()
		})

		AfterEach(func() {
			presence.Remove()
		})

		It("should put /key/value in the store with a TTL", func() {
			Eventually(func() bool { return locked }).Should(BeTrue())

			node, err := store.Get(key)
			Ω(err).ShouldNot(HaveOccurred())
			Ω(node).Should(Equal(storeadapter.StoreNode{
				Key:   key,
				Value: []byte(value),
				TTL:   uint64(interval.Seconds()), // move to config one day
			}))

		})

		It("should reacquire the presence", func() {
			Eventually(func() bool { return locked }).Should(BeTrue())

			err = store.Delete(key)
			Ω(err).ShouldNot(HaveOccurred())
			Consistently(func() bool { return locked }, 2).Should(BeTrue())
		})

		It("should fail if we maintain presence multiple times", func() {
			_, err = presence.Maintain(interval)
			Ω(err).Should(HaveOccurred())
		})
	})

	Describe("Remove", func() {
		It("should remove the presence", func() {
			status, err := presence.Maintain(interval)
			Eventually(status).Should(Receive())
			presence.Remove()

			Eventually(func() error {
				_, err = store.Get(key)
				return err
			}, 2).Should(Equal(storeadapter.ErrorKeyNotFound))
		})

		It("should close the status channel", func() {
			status, err := presence.Maintain(interval)
			Ω(err).ShouldNot(HaveOccurred())
			Eventually(status).Should(Receive())

			presence.Remove()
			Eventually(status).Should(BeClosed())
		})
	})
})
