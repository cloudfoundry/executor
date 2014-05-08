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

var _ = Context("Metrics BBS", func() {
	var bbs *BBS
	var timeProvider *faketimeprovider.FakeTimeProvider

	BeforeEach(func() {
		timeProvider = faketimeprovider.New(time.Unix(1238, 0))
		bbs = New(etcdClient, timeProvider)
	})

	Describe("GetServiceRegistrations", func() {
		var registrations models.ServiceRegistrations
		var registrationsErr error

		JustBeforeEach(func() {
			registrations, registrationsErr = bbs.GetServiceRegistrations()
		})

		Context("when etcd returns sucessfully", func() {
			BeforeEach(func() {
				serviceNodes := []storeadapter.StoreNode{
					{
						Key: "/v1/executor/guid-0",
					},
					{
						Key: "/v1/executor/guid-1",
					},
					{
						Key:   "/v1/file_server/guid-0",
						Value: []byte("http://example.com/file-server"),
					},
				}
				err := etcdClient.SetMulti(serviceNodes)
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("does not return an error", func() {
				Ω(registrationsErr).ShouldNot(HaveOccurred())
			})

			It("returns the executor service registrations", func() {
				executorRegistrations := registrations.FilterByName(models.ExecutorServiceName)
				Ω(executorRegistrations).Should(HaveLen(2))
				Ω(executorRegistrations).Should(ContainElement(models.ServiceRegistration{
					Name: models.ExecutorServiceName, Id: "guid-0",
				}))
				Ω(executorRegistrations).Should(ContainElement(models.ServiceRegistration{
					Name: models.ExecutorServiceName, Id: "guid-1",
				}))
			})

			It("returns the file-server service registrations", func() {
				Ω(registrations.FilterByName(models.FileServerServiceName)).Should(Equal(models.ServiceRegistrations{
					{Name: models.FileServerServiceName, Id: "guid-0", Location: "http://example.com/file-server"},
				}))
			})
		})

		Context("when etcd comes up empty", func() {
			It("does not return an error", func() {
				Ω(registrationsErr).ShouldNot(HaveOccurred())
			})

			It("returns empty registrations", func() {
				Ω(registrations).Should(BeEmpty())
			})
		})
	})
})
