package executor_test

import (
	"github.com/cloudfoundry-incubator/executor"
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
})
