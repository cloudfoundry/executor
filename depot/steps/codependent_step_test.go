package steps_test

import (
	"errors"
	"sync"

	"github.com/hashicorp/go-multierror"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/cloudfoundry-incubator/executor/depot/steps"
	"github.com/cloudfoundry-incubator/executor/depot/steps/fakes"
)

var _ = Describe("CodependentStep", func() {
	var step steps.Step
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
		step = steps.NewCodependent([]steps.Step{subStep1, subStep2})
	})

	Describe("Perform", func() {
		It("performs its substeps in parallel", func() {
			err := step.Perform()
			Expect(err).NotTo(HaveOccurred())

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

			It("returns an aggregate of the failures", func() {
				err := step.Perform()
				Expect(err.(*multierror.Error).WrappedErrors()).To(ConsistOf(disaster))
			})

			It("cancels all the steps", func() {
				step.Perform()

				Expect(subStep1.CancelCallCount()).To(Equal(1))
				Expect(subStep2.CancelCallCount()).To(Equal(1))
			})
		})
	})

	Describe("Cancel", func() {
		It("cancels all sub-steps", func() {
			step1 := &fakes.FakeStep{}
			step2 := &fakes.FakeStep{}
			step3 := &fakes.FakeStep{}

			sequence := steps.NewCodependent([]steps.Step{step1, step2, step3})

			sequence.Cancel()

			Expect(step1.CancelCallCount()).To(Equal(1))
			Expect(step2.CancelCallCount()).To(Equal(1))
			Expect(step3.CancelCallCount()).To(Equal(1))
		})
	})
})
