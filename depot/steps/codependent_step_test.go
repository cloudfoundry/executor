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
	var subStep1 Step
	var subStep2 Step

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

	It("performs its substeps in parallel", func(done Done) {
		defer close(done)

		err := step.Perform()
		Ω(err).ShouldNot(HaveOccurred())

		Eventually(thingHappened).Should(Receive())
		Eventually(thingHappened).Should(Receive())
	}, 2)

	Context("when one of the substeps fails", func() {
		disaster := errors.New("oh no!")

		var step2Canceled chan struct{}

		BeforeEach(func() {
			step2Canceled = make(chan struct{})

			subStep1 = &fakes.FakeStep{
				PerformStub: func() error {
					return disaster
				},
			}

			subStep2 = &fakes.FakeStep{
				PerformStub: func() error {
					return nil
				},

				CancelStub: func() {
					close(step2Canceled)
				},
			}
		})

		It("cancels the rest of the steps", func() {
			err := step.Perform()
			Ω(err).Should(Equal(disaster))

			Eventually(step2Canceled).Should(BeClosed())
		})
	})

	Context("when told to cancel", func() {
		var (
			step1Canceled   chan struct{}
			step2Canceled   chan struct{}
			finishCanceling chan struct{}

			performing *sync.WaitGroup
			canceled   chan struct{}
		)

		BeforeEach(func() {
			step1Canceled = make(chan struct{})
			step2Canceled = make(chan struct{})
			finishCanceling = make(chan struct{})
			canceled = make(chan struct{})

			performing = new(sync.WaitGroup)
			performing.Add(2)

			subStep1 = &fakes.FakeStep{
				PerformStub: func() error {
					performing.Done()
					<-step1Canceled
					return nil
				},

				CancelStub: func() {
					close(step1Canceled)
					<-finishCanceling
				},
			}

			subStep2 = &fakes.FakeStep{
				PerformStub: func() error {
					performing.Done()
					<-step2Canceled
					return nil
				},

				CancelStub: func() {
					close(step2Canceled)
					<-finishCanceling
				},
			}
		})

		Context("while performing", func() {
			var performErr <-chan error

			JustBeforeEach(func() {
				errs := make(chan error)
				performErr = errs

				go func() {
					errs <- step.Perform()
				}()

				performing.Wait()

				go func() {
					step.Cancel()
					close(canceled)
				}()
			})

			It("cancels the running steps in parallel, and waits for them to complete before exiting", func() {
				Eventually(step1Canceled).Should(BeClosed())
				Eventually(step2Canceled).Should(BeClosed())

				Consistently(performErr).ShouldNot(Receive())
				Consistently(canceled).ShouldNot(BeClosed())

				close(finishCanceling)

				Eventually(performErr).Should(Receive())
				Eventually(canceled).Should(BeClosed())
			})
		})
	})
})
