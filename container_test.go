package executor_test

import (
	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Container", func() {
	Describe("HasTags", func() {
		var container executor.Container

		Context("when tags are nil", func() {
			BeforeEach(func() {
				container = executor.Container{
					Tags: nil,
				}
			})

			It("returns true if requested tags are nil", func() {
				Expect(container.HasTags(nil)).To(BeTrue())
			})

			It("returns false if requested tags are not nil", func() {
				Expect(container.HasTags(executor.Tags{"a": "b"})).To(BeFalse())
			})
		})

		Context("when tags are not nil", func() {
			BeforeEach(func() {
				container = executor.Container{
					Tags: executor.Tags{"a": "b"},
				}
			})

			It("returns true when found", func() {
				Expect(container.HasTags(executor.Tags{"a": "b"})).To(BeTrue())
			})

			It("returns false when nil", func() {
				Expect(container.HasTags(nil)).To(BeFalse())
			})

			It("returns false when not found", func() {
				Expect(container.HasTags(executor.Tags{"a": "c"})).To(BeFalse())
			})
		})
	})

	Describe("UnmarshalJSON", func() {
		var securityGroupRule models.SecurityGroupRule
		var payload string
		var container executor.Container
		BeforeEach(func() {
			container = executor.Container{}

			payload = `{
				    "setup":null,
						"run":null,
						"monitor":null,
						"egress_rules":[
		          {
				        "protocol": "tcp",
								"destinations": ["0.0.0.0/0"],
				        "port_range": {
					        "start": 1,
					        "end": 1024
				        }
			        }
		        ]
					}`
			securityGroupRule = models.SecurityGroupRule{
				Protocol:     "tcp",
				Destinations: []string{"0.0.0.0/0"},
				PortRange: &models.PortRange{
					Start: 1,
					End:   1024,
				},
			}
		})

		Context("when it has egress rules", func() {
			It("unmarshals egress rules", func() {
				err := container.UnmarshalJSON([]byte(payload))
				Expect(err).NotTo(HaveOccurred())
				Expect(container.EgressRules).To(HaveLen(1))
				Expect(container.EgressRules[0]).To(Equal(securityGroupRule))
			})
		})
	})

})
