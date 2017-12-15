package steps_test

import (
	"errors"

	"code.cloudfoundry.org/executor/depot/steps"
	"code.cloudfoundry.org/executor/depot/steps/fakes"
	"code.cloudfoundry.org/lager/lagertest"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("BackgroundStep", func() {
	var (
		substepPerformError error
		substep             *fakes.FakeStep
		logger              *lagertest.TestLogger
	)

	BeforeEach(func() {
		logger = lagertest.NewTestLogger("test")
	})

	Describe("Perform", func() {
		var (
			err error
		)

		BeforeEach(func() {
			substep = &fakes.FakeStep{
				PerformStub: func() error {
					return substepPerformError
				},
			}
		})

		JustBeforeEach(func() {
			err = steps.NewBackground(substep, logger).Perform()
		})

		Context("when the substep returns an error", func() {
			BeforeEach(func() {
				substepPerformError = errors.New("some error")
			})

			It("performs the substep", func() {
				Expect(substep.PerformCallCount()).To(Equal(1))
			})

			It("returns this error", func() {
				Expect(err).To(Equal(substepPerformError))
			})
		})

		Context("when the substep does not error", func() {
			BeforeEach(func() {
				substepPerformError = nil
			})

			It("performs the substep", func() {
				Expect(substep.PerformCallCount()).To(Equal(1))
			})

			It("does not error", func() {
				Expect(err).NotTo(HaveOccurred())
			})
		})
	})

	Describe("Cancel", func() {
		var (
			errs          chan error
			step          steps.Step
			blkChannel    chan struct{}
			calledChannel chan struct{}
		)

		BeforeEach(func() {
			errs = make(chan error, 10)
			calledChannel = make(chan struct{}, 10)
			blkChannel = make(chan struct{})
			substep = &fakes.FakeStep{
				PerformStub: func() error {
					calledChannel <- struct{}{}
					<-blkChannel
					return nil
				},
			}
			step = steps.NewBackground(substep, logger)
		})

		JustBeforeEach(func() {
			go func() {
				errs <- step.Perform()
			}()
		})

		It("never cancels the substep", func() {
			Eventually(calledChannel).Should(Receive())
			step.Cancel()
			Eventually(errs).Should(Receive())
			Expect(substep.CancelCallCount()).To(BeZero())
		})
	})
})
