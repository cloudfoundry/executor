package executor_test

import (
	. "github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Container", func() {
	Describe("HasTags", func() {
		var container Container

		Context("when tags are nil", func() {
			BeforeEach(func() {
				container = Container{
					Tags: nil,
				}
			})

			It("returns true if requested tags are nil", func() {
				Ω(container.HasTags(nil)).Should(BeTrue())
			})

			It("returns false if requested tags are not nil", func() {
				Ω(container.HasTags(Tags{"a": "b"})).Should(BeFalse())
			})
		})

		Context("when tags are not nil", func() {
			BeforeEach(func() {
				container = Container{
					Tags: Tags{"a": "b"},
				}
			})

			It("returns true when found", func() {
				Ω(container.HasTags(Tags{"a": "b"})).Should(BeTrue())
			})

			It("returns false when nil", func() {
				Ω(container.HasTags(nil)).Should(BeFalse())
			})

			It("returns false when not found", func() {
				Ω(container.HasTags(Tags{"a": "c"})).Should(BeFalse())
			})
		})
	})

	Describe("UnmarshalJSON", func() {
		var securityGroupRule models.SecurityGroupRule
		var payload string
		var container Container
		BeforeEach(func() {
			container = Container{}

			payload = `{
				    "setup":null,
						"run":null,
						"monitor":null,
						"security_group_rules":[
		          {
				        "protocol": "tcp",
								"destination": "0.0.0.0/0",
				        "port_range": {
					        "start": 1,
					        "end": 1024
				        }
			        }
		        ]
					}`
			securityGroupRule = models.SecurityGroupRule{
				Protocol:    "tcp",
				Destination: "0.0.0.0/0",
				PortRange: &models.PortRange{
					Start: 1,
					End:   1024,
				},
			}
		})

		Context("when it has security group rules", func() {
			It("unmarshals security group rules", func() {
				err := container.UnmarshalJSON([]byte(payload))
				Ω(err).ShouldNot(HaveOccurred())
				Ω(container.SecurityGroupRules).Should(HaveLen(1))
				Ω(container.SecurityGroupRules[0]).Should(Equal(securityGroupRule))
			})
		})
	})

})
