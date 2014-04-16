package bbs_test

import (
	"time"

	"github.com/cloudfoundry/gunk/timeprovider/faketimeprovider"
	"github.com/cloudfoundry/storeadapter"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	. "github.com/cloudfoundry-incubator/runtime-schema/bbs"
)

var _ = Context("Servistry BBS", func() {
	var bbs *BBS
	var timeProvider *faketimeprovider.FakeTimeProvider
	var host = "1.2.3.4"
	var port = 8080
	var etcdKey = "/v1/cloud_controller/1.2.3.4:8080"
	var etcdValue = "http://1.2.3.4:8080"
	var ttl = 120 * time.Second

	BeforeEach(func() {
		timeProvider = faketimeprovider.New(time.Unix(1238, 0))
		bbs = New(store, timeProvider)
	})

	Describe("RegisterCC", func() {
		var registerErr error

		JustBeforeEach(func() {
			registerErr = bbs.RegisterCC(host, port, ttl)
		})

		Context("when the registration does not exist", func() {
			It("does not return an error", func() {
				Ω(registerErr).ShouldNot(HaveOccurred())
			})

			It("creates /cloud_controller/<host:ip>", func() {
				node, err := store.Get(etcdKey)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(node.Value).Should(Equal([]byte(etcdValue)))
			})
		})

		Context("when the registration does exist", func() {
			BeforeEach(func() {
				err := bbs.RegisterCC(host, port, 20*time.Second)
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("does not return an error", func() {
				Ω(registerErr).ShouldNot(HaveOccurred())
			})
			It("updates the ttl", func() {
				node, err := store.Get(etcdKey)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(node.TTL).Should(BeNumerically("~", 120, 3))
			})
		})

		Context("when registration fails", func() {
			BeforeEach(func() {
				etcdRunner.Stop()
			})

			It("returns an error", func() {
				Ω(registerErr).Should(HaveOccurred())
			})
		})
	})

	Describe("UnregisterCC", func() {
		var unregisterErr error

		JustBeforeEach(func() {
			unregisterErr = bbs.UnregisterCC(host, port)
		})

		Context("when the registration exists", func() {
			BeforeEach(func() {
				err := bbs.RegisterCC(host, port, ttl)
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("does not return an error", func() {
				Ω(unregisterErr).ShouldNot(HaveOccurred())
			})

			It("deletes /cloud_controller/<host:ip>", func() {
				_, err := store.Get(etcdKey)
				Ω(err).Should(Equal(storeadapter.ErrorKeyNotFound))
			})
		})

		Context("when the registration does not exist", func() {
			It("does not return an error", func() {
				Ω(unregisterErr).ShouldNot(HaveOccurred())
			})
		})

		Context("when unregistration fails", func() {
			BeforeEach(func() {
				etcdRunner.Stop()
			})

			It("returns an error", func() {
				Ω(unregisterErr).Should(HaveOccurred())
			})
		})
	})

	Describe("GetAvailableCC", func() {
		BeforeEach(func() {
			err := bbs.RegisterCC(host, port, ttl)
			Ω(err).ShouldNot(HaveOccurred())
		})

		It("returns the urls of registered cloud controllers", func() {
			urls, err := bbs.GetAvailableCC()
			Ω(err).ShouldNot(HaveOccurred())
			Ω(urls).Should(HaveLen(1))
			Ω(urls[0]).Should(Equal(etcdValue))
		})
	})
})
