package services_bbs_test

import (
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	. "github.com/cloudfoundry-incubator/runtime-schema/bbs/services_bbs"
	"github.com/cloudfoundry/storeadapter"
	. "github.com/cloudfoundry/storeadapter/storenodematchers"
	"github.com/cloudfoundry/storeadapter/test_helpers"
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

		presence = NewPresence(etcdClient, key, []byte(value))
		interval = 1 * time.Second
	})

	Describe("Maintain", func() {
		var reporter *test_helpers.StatusReporter

		BeforeEach(func() {
			status, err := presence.Maintain(interval)
			Ω(err).ShouldNot(HaveOccurred())

			reporter = test_helpers.NewStatusReporter(status)
		})

		AfterEach(func() {
			presence.Remove()
		})

		It("should put /key/value in the store with a TTL", func() {
			Eventually(reporter.Locked).Should(BeTrue())

			node, err := etcdClient.Get(key)
			Ω(err).ShouldNot(HaveOccurred())
			Ω(node).Should(MatchStoreNode(storeadapter.StoreNode{
				Key:   key,
				Value: []byte(value),
				TTL:   uint64(interval.Seconds()), // move to config one day
			}))

		})

		It("should fail if we maintain presence multiple times", func() {
			_, err := presence.Maintain(interval)
			Ω(err).Should(HaveOccurred())
		})

		Context("when presence is lost", func() {
			It("eventually reacquires it", func() {
				Eventually(reporter.Locked).Should(BeTrue())

				err := etcdClient.Delete(key)
				Ω(err).ShouldNot(HaveOccurred())

				time.Sleep(interval * 2)

				Ω(reporter.Locked()).Should(BeTrue())
			})
		})
	})

	Describe("Remove", func() {
		It("should remove the presence", func() {
			status, err := presence.Maintain(interval)
			Eventually(status).Should(Receive())
			presence.Remove()

			Eventually(func() error {
				_, err = etcdClient.Get(key)
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
