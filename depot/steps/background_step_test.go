package steps_test

import (
	"errors"
	"os"

	"code.cloudfoundry.org/executor/depot/steps"
	"code.cloudfoundry.org/lager/lagertest"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/tedsuo/ifrit"
)

// we should probably switch over to use counterfieter for this... i initially thought this might be simpler, but i'm not sure that's true
type fakeRunner struct {
	runCallCount, signalCount int
	runErr                    error
}

func (fr *fakeRunner) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	fr.runCallCount++
	if signals != nil {
		<-signals
		fr.signalCount++
	}
	return fr.runErr
}

var _ = FDescribe("BackgroundStep", func() {
	var (
		substepPerformError error
		substep             *fakeRunner
		logger              *lagertest.TestLogger
	)

	BeforeEach(func() {
		logger = lagertest.NewTestLogger("test")
	})

	Describe("Perform", func() {
		var (
			err error
		)

		JustBeforeEach(func() {
			substep = &fakeRunner{runErr: substepPerformError}
			err = steps.NewBackground(substep, logger).Run(nil, nil)
		})

		Context("when the substep returns an error", func() {
			BeforeEach(func() {
				substepPerformError = errors.New("some error")
			})

			It("performs the substep", func() {
				Expect(substep.runCallCount).To(Equal(1))
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
				Expect(substep.runCallCount).To(Equal(1))
			})

			It("does not error", func() {
				Expect(err).NotTo(HaveOccurred())
			})
		})
	})

	Describe("Cancel", func() {
		var (
			errs    chan error
			signals chan os.Signal
			step    ifrit.Runner
		)

		BeforeEach(func() {
			errs = make(chan error, 1)
			signals = make(chan os.Signal, 1)
			substep = &fakeRunner{}
			step = steps.NewBackground(substep, logger)
		})

		JustBeforeEach(func() {
			go func() {
				errs <- step.Run(signals, nil)
			}()
		})

		It("never cancels the substep", func() {
			Eventually(substep.runCallCount).Should(Equal(1)) // I HAVE NO IDEA WHY THIS IS FAILING!
			signals <- os.Interrupt
			Eventually(errs).Should(Receive())
			Expect(substep.signalCount).To(BeZero())
		})
	})
})
