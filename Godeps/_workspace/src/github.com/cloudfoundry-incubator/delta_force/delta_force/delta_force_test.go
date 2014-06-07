package delta_force_test

import (
	. "github.com/cloudfoundry-incubator/delta_force/delta_force"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("DeltaForce", func() {
	var (
		numDesired int
		actual     []ActualInstance
	)

	Context("When the actual state matches the desired state", func() {
		BeforeEach(func() {
			numDesired = 3
			actual = []ActualInstance{
				{0, "a"},
				{1, "b"},
				{2, "c"},
			}
		})

		It("returns an empty result", func() {
			result := Reconcile(numDesired, actual)
			Ω(result.IndicesToStart).Should(BeEmpty())
			Ω(result.GuidsToStop).Should(BeEmpty())
			Ω(result.IndicesToStopOneGuid).Should(BeEmpty())
		})
	})

	Context("When the number of desired instances is less than the actual number of instances", func() {
		BeforeEach(func() {
			numDesired = 1
			actual = []ActualInstance{
				{0, "a"},
				{1, "b"},
				{2, "c"},
				{2, "d"},
			}
		})

		It("instructs the caller to stop the extra guids", func() {
			result := Reconcile(numDesired, actual)
			Ω(result.IndicesToStart).Should(BeEmpty())
			Ω(result.GuidsToStop).Should(Equal([]string{"b", "c", "d"}))
			Ω(result.IndicesToStopOneGuid).Should(BeEmpty())
		})
	})

	Context("When the number of desired instances is greater than the actual number of instances", func() {
		BeforeEach(func() {
			numDesired = 5
			actual = []ActualInstance{
				{0, "a"},
				{1, "b"},
				{2, "c"},
			}
		})

		It("instructs the caller to start the missing indices", func() {
			result := Reconcile(numDesired, actual)
			Ω(result.IndicesToStart).Should(Equal([]int{3, 4}))
			Ω(result.GuidsToStop).Should(BeEmpty())
			Ω(result.IndicesToStopOneGuid).Should(BeEmpty())
		})
	})

	Context("When the indices are not contiguous", func() {
		BeforeEach(func() {
			numDesired = 4
			actual = []ActualInstance{
				{0, "a"},
				{0, "b"},
				{1, "c"},
				{2, "d"},
				{4, "e"},
			}
		})

		It("instructs the caller to start the missing indices, but not to stop any extra indices", func() {
			result := Reconcile(numDesired, actual)
			Ω(result.IndicesToStart).Should(Equal([]int{3}))
			Ω(result.GuidsToStop).Should(BeEmpty())
			Ω(result.IndicesToStopOneGuid).Should(BeEmpty())
		})
	})

	Context("when there are multiple instances on a desired index", func() {
		BeforeEach(func() {
			numDesired = 3
			actual = []ActualInstance{
				{0, "a"},
				{0, "b"},
				{0, "c"},
				{1, "d"},
				{2, "e"},
				{2, "f"},
				{3, "g"},
				{3, "h"},
			}
		})

		It("instructs the caller to stop one of the instances at that index", func() {
			result := Reconcile(numDesired, actual)
			Ω(result.IndicesToStart).Should(BeEmpty())
			Ω(result.GuidsToStop).Should(Equal([]string{"g", "h"}))
			Ω(result.IndicesToStopOneGuid).Should(Equal([]int{0, 2}))
		})
	})

	Describe("Result", func() {
		Context("when empty", func() {
			It("should say so", func() {
				result := Result{}
				Ω(result.Empty()).Should(BeTrue())

				result = Result{
					IndicesToStart:       []int{},
					GuidsToStop:          []string{},
					IndicesToStopOneGuid: []int{},
				}
				Ω(result.Empty()).Should(BeTrue())

				result = Result{
					IndicesToStart:       []int{1},
					GuidsToStop:          []string{},
					IndicesToStopOneGuid: []int{},
				}
				Ω(result.Empty()).Should(BeFalse())

				result = Result{
					IndicesToStart:       []int{},
					GuidsToStop:          []string{"foo"},
					IndicesToStopOneGuid: []int{},
				}
				Ω(result.Empty()).Should(BeFalse())

				result = Result{
					IndicesToStart:       []int{},
					GuidsToStop:          []string{},
					IndicesToStopOneGuid: []int{1},
				}
				Ω(result.Empty()).Should(BeFalse())
			})
		})
	})
})
