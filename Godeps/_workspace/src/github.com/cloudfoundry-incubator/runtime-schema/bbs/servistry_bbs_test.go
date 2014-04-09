package bbs_test

import (
	"time"

	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/gunk/timeprovider/faketimeprovider"
	"github.com/cloudfoundry/storeadapter"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	. "github.com/cloudfoundry-incubator/runtime-schema/bbs"
)

var _ = Context("Servistry BBS", func() {
	var bbs *BBS
	var timeProvider *faketimeprovider.FakeTimeProvider
	var expectedRegistration = models.CCRegistrationMessage{
		Host: "1.2.3.4",
		Port: 8080,
	}
	var ttl = 120 * time.Second

	BeforeEach(func() {
		timeProvider = faketimeprovider.New(time.Unix(1238, 0))
		bbs = New(store, timeProvider)
	})

	Describe("RegisterCC", func() {
		var registerErr error

		JustBeforeEach(func() {
			registerErr = bbs.RegisterCC(expectedRegistration, ttl)
		})

		Context("when the registration does not exist", func() {
			It("does not return an error", func() {
				Ω(registerErr).ShouldNot(HaveOccurred())
			})

			It("creates /cloud_controller/<host:ip>", func() {
				node, err := store.Get("/v1/cloud_controller/1.2.3.4:8080")
				Ω(err).ShouldNot(HaveOccurred())
				Ω(node.Value).Should(Equal([]byte("http://1.2.3.4:8080")))
			})
		})

		Context("when the registration does exist", func() {
			BeforeEach(func() {
				err := bbs.RegisterCC(expectedRegistration, 20*time.Second)
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("does not return an error", func() {
				Ω(registerErr).ShouldNot(HaveOccurred())
			})
			It("updates the ttl", func() {
				node, err := store.Get("/v1/cloud_controller/1.2.3.4:8080")
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
			unregisterErr = bbs.UnregisterCC(expectedRegistration)
		})

		Context("when the registration exists", func() {
			BeforeEach(func() {
				err := bbs.RegisterCC(expectedRegistration, ttl)
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("does not return an error", func() {
				Ω(unregisterErr).ShouldNot(HaveOccurred())
			})

			It("deletes /cloud_controller/<host:ip>", func() {
				_, err := store.Get("/v1/cloud_controller/1.2.3.4:8080")
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
})
