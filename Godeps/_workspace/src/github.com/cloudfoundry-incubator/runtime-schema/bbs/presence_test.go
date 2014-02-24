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
		presence *Presence
		key      string
		value    string
		interval uint64
		errors   chan error
		err      error
	)

	BeforeEach(func() {
		key = "/v1/some-key"
		value = "some-value"

		presence = NewPresence(store, key, []byte(value))
		interval = uint64(1)

		errors, err = presence.Maintain(interval)
		Ω(err).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		presence.Remove()
	})

	Describe("Maintain", func() {
		It("should put /key/value in the store with a TTL", func() {
			node, err := store.Get(key)
			Ω(err).ShouldNot(HaveOccurred())
			Ω(node).Should(Equal(storeadapter.StoreNode{
				Key:   key,
				Value: []byte(value),
				TTL:   interval, // move to config one day
			}))

		})

		It("should periodically maintain the TTL", func() {
			time.Sleep(2 * time.Second)

			_, err = store.Get(key)
			Ω(err).ShouldNot(HaveOccurred())
		})

		It("should report an error and stop trying if it fails to update the TTL", func() {
			err = store.Delete(key)
			Ω(err).ShouldNot(HaveOccurred())

			Eventually(errors, 2).Should(Receive())
		})

		It("should fail if we maintain presence multiple times", func() {
			_, err = presence.Maintain(interval)
			Ω(err).Should(HaveOccurred())
		})
	})

	Describe("Remove", func() {
		It("should remove the presence", func() {
			presence.Remove()

			Eventually(func() error {
				_, err = store.Get(key)
				return err
			}, 2).Should(Equal(storeadapter.ErrorKeyNotFound))
		})

		It("should not report an error", func() {
			presence.Remove()
			Eventually(errors, 2).ShouldNot(Receive())
		})
	})
})
