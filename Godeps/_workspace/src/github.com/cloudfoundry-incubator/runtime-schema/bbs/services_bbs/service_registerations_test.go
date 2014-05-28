package services_bbs_test

import (
	"time"

	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/storeadapter"
	"github.com/cloudfoundry/storeadapter/test_helpers"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	. "github.com/cloudfoundry-incubator/runtime-schema/bbs/services_bbs"
	steno "github.com/cloudfoundry/gosteno"
)

var _ = Context("Getting Generic Services", func() {
	var bbs *ServicesBBS

	BeforeEach(func() {
		logSink := steno.NewTestingSink()

		steno.Init(&steno.Config{
			Sinks: []steno.Sink{logSink},
		})

		logger := steno.NewLogger("the-logger")
		steno.EnterTestMode()

		bbs = New(etcdClient, logger)
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

	Describe("GetAllExecutors", func() {
		It("returns a list of the executor IDs that exist", func() {
			executors, err := bbs.GetAllExecutors()
			Ω(err).ShouldNot(HaveOccurred())

			Ω(executors).Should(BeEmpty())

			presenceA, statusA, err := bbs.MaintainExecutorPresence(1*time.Second, "executor-a")
			Ω(err).ShouldNot(HaveOccurred())
			test_helpers.NewStatusReporter(statusA)

			presenceB, statusB, err := bbs.MaintainExecutorPresence(1*time.Second, "executor-b")
			Ω(err).ShouldNot(HaveOccurred())
			test_helpers.NewStatusReporter(statusB)

			Eventually(func() []string {
				executors, _ := bbs.GetAllExecutors()
				return executors
			}).Should(ContainElement("executor-a"))

			Eventually(func() []string {
				executors, _ := bbs.GetAllExecutors()
				return executors
			}).Should(ContainElement("executor-b"))

			presenceA.Remove()
			presenceB.Remove()
		})
	})
})
