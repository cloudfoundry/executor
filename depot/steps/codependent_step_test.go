package steps_test

import (
	"errors"
	"sync"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	. "github.com/cloudfoundry-incubator/executor/depot/steps"
	"github.com/cloudfoundry-incubator/executor/depot/steps/fakes"
)

var _ = Describe("CodependentStep", func() {
	var step Step
	var subStep1 *fakes.FakeStep
	var subStep2 *fakes.FakeStep

	var thingHappened chan bool
	var cancelled chan bool

	BeforeEach(func() {
		thingHappened = make(chan bool, 2)
		cancelled = make(chan bool, 2)

		running := new(sync.WaitGroup)
		running.Add(2)

		subStep1 = &fakes.FakeStep{
			PerformStub: func() error {
				running.Done()
				running.Wait()
				thingHappened <- true
				return nil
			},
			CancelStub: func() {
				cancelled <- true
			},
		}

		subStep2 = &fakes.FakeStep{
			PerformStub: func() error {
				running.Done()
				running.Wait()
				thingHappened <- true
				return nil
			},
			CancelStub: func() {
				cancelled <- true
			},
		}
	})

	JustBeforeEach(func() {
		step = NewCodependent([]Step{subStep1, subStep2})
	})

	Describe("Perform", func() {
		It("performs its substeps in parallel", func() {
			err := step.Perform()
			Ω(err).ShouldNot(HaveOccurred())

			Eventually(thingHappened).Should(Receive())
			Eventually(thingHappened).Should(Receive())
		})

		Context("when one of the substeps fails", func() {
			disaster := errors.New("oh no!")

			BeforeEach(func() {
				subStep1 = &fakes.FakeStep{
					PerformStub: func() error {
						return disaster
					},
				}

				subStep2 = &fakes.FakeStep{
					PerformStub: func() error {
						return nil
					},
				}
			})

			It("returns the first failure", func() {
				err := step.Perform()
				Ω(err).Should(Equal(disaster))
			})

			It("cancels all the step", func() {
				step.Perform()

				Ω(subStep1.CancelCallCount()).Should(Equal(1))
				Ω(subStep2.CancelCallCount()).Should(Equal(1))
			})
		})
	})

	Describe("Cancel", func() {
		It("cancels all sub-steps", func() {
			step1 := &fakes.FakeStep{}
			step2 := &fakes.FakeStep{}
			step3 := &fakes.FakeStep{}

			sequence := NewCodependent([]Step{step1, step2, step3})

			sequence.Cancel()

			Ω(step1.CancelCallCount()).Should(Equal(1))
			Ω(step2.CancelCallCount()).Should(Equal(1))
			Ω(step3.CancelCallCount()).Should(Equal(1))
		})
	})
})
