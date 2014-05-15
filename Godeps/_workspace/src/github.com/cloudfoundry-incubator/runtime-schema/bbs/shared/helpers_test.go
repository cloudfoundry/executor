package shared_test

import (
	. "github.com/cloudfoundry-incubator/runtime-schema/bbs/shared"
	"github.com/cloudfoundry/storeadapter"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Shared", func() {
	Describe("#RetryIndefinitelyOnStoreTimeout", func() {
		Context("when the callback returns a storeadapter.ErrorTimeout", func() {
			It("should call the callback again", func() {
				numCalls := 0

				callback := func() error {
					numCalls++
					if numCalls == 1 {
						return storeadapter.ErrorTimeout
					}
					return nil
				}

				RetryIndefinitelyOnStoreTimeout(callback)
				Ω(numCalls).Should(Equal(2))
			})
		})

		Context("when the callback does not return a storeadapter.ErrorTimeout", func() {
			It("should call the callback once", func() {
				numCalls := 0

				callback := func() error {
					numCalls++
					return nil
				}

				RetryIndefinitelyOnStoreTimeout(callback)
				Ω(numCalls).Should(Equal(1))
			})
		})
	})
})
