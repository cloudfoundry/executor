package emittable_error_test

import (
	"errors"
	. "github.com/cloudfoundry-incubator/executor/depot/steps/emittable_error"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("EmittableError", func() {
	wrappedError := errors.New("the wrapped error")

	It("should satisfy the error interface", func() {
		var err error
		err = New(wrappedError, "Fancy")
		Ω(err).Should(HaveOccurred())
	})

	Describe("Error", func() {
		It("should include the emittable message and the wrapped error's message", func() {
			err := New(wrappedError, "Fancy emittable message")
			Ω(err.Error()).Should(Equal("Fancy emittable message\nthe wrapped error"))
		})

		Context("when the wrapped error is nil", func() {
			It("should not blow up", func() {
				err := New(nil, "Fancy emittable message")
				Ω(err.Error()).Should(Equal("Fancy emittable message"))
			})
		})
	})

	Describe("EmittableError", func() {
		Context("with no format args", func() {
			It("should just be the message", func() {
				Ω(New(wrappedError, "Fancy %s %d").EmittableError()).Should(Equal("Fancy %s %d"))
			})
		})

		Context("with format args", func() {
			It("should Sprintf", func() {
				Ω(New(wrappedError, "Fancy %s %d", "hi", 3).EmittableError()).Should(Equal("Fancy hi 3"))
			})
		})
	})
})
