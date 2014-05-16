package checks_test

import (
	"encoding/json"
	. "github.com/cloudfoundry-incubator/executor/checks"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Check", func() {
	Context("with a bogus payload", func() {
		It("returns an error", func() {
			_, err := Parse(json.RawMessage(`{"name":"`))
			Ω(err).Should(HaveOccurred())
		})
	})

	Context("with a dial check", func() {
		It("constructs a dial check", func() {
			check, err := Parse(json.RawMessage(`{"name":"dial","args":{"network":"tcp","addr":":8080"}}`))
			Ω(err).ShouldNot(HaveOccurred())

			Ω(check).Should(Equal(NewDial("tcp", ":8080")))
		})

		Context("with bogus args", func() {
			It("constructs a dial check", func() {
				_, err := Parse(json.RawMessage(`{"name":"dial","args":"lol"}`))
				Ω(err).Should(HaveOccurred())
			})
		})
	})

	Context("with a mysterious check", func() {
		It("returns an error", func() {
			_, err := Parse(json.RawMessage(`{"name":"mysterious"}`))
			Ω(err).Should(Equal(ErrUnknownCheck{"mysterious"}))
		})
	})
})
