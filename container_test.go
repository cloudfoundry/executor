package executor_test

import (
	. "github.com/cloudfoundry-incubator/executor"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Container", func() {
	Describe("HasTags", func() {
		var container Container

		Context("when tags are nil", func() {
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
})
