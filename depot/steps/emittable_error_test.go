package steps_test

import (
	"errors"

	. "github.com/cloudfoundry-incubator/executor/depot/steps"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("EmittableError", func() {
	wrappedError := errors.New("the wrapped error")

	It("should satisfy the error interface", func() {
		var err error
		err = NewEmittableError(wrappedError, "Fancy")
		Ω(err).Should(HaveOccurred())
	})

	Describe("WrappedError", func() {
		It("returns the wrapped error message", func() {
			err := NewEmittableError(wrappedError, "Fancy emittable message")
			Ω(err.WrappedError()).Should(Equal(wrappedError))
		})

		Context("when the wrapped error is nil", func() {
			It("should not blow up", func() {
				err := NewEmittableError(nil, "Fancy emittable message")
				Ω(err.WrappedError()).Should(BeNil())
			})
		})
	})

	Describe("Error", func() {
		Context("with no format args", func() {
			It("should just be the message", func() {
				Ω(NewEmittableError(wrappedError, "Fancy %s %d").Error()).Should(Equal("Fancy %s %d"))
			})
		})

		Context("with format args", func() {
			It("should Sprintf", func() {
				Ω(NewEmittableError(wrappedError, "Fancy %s %d", "hi", 3).Error()).Should(Equal("Fancy hi 3"))
			})
		})
	})
})
