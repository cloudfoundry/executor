package steps_test

import (
	"errors"
	"os"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/fake_runner"

	"code.cloudfoundry.org/executor/depot/steps"
)

var _ = Describe("SerialStep", func() {
	var (
		testRunner1, testRunner2, testRunner3 *fake_runner.TestRunner
		sequence                              ifrit.Runner
		p                                     ifrit.Process
	)
	BeforeEach(func() {
		testRunner1 = fake_runner.NewTestRunner()
		testRunner2 = fake_runner.NewTestRunner()
		testRunner3 = fake_runner.NewTestRunner()
		sequence = steps.NewSerial([]ifrit.Runner{
			testRunner1,
			testRunner2,
			testRunner3,
		})
		p = ifrit.Background(sequence)
	})
	AfterEach(func() {
		testRunner1.EnsureExit()
		testRunner2.EnsureExit()
		testRunner3.EnsureExit()
	})

	Describe("Run", func() {
		It("closes the ready channel immediately", func() {
			Eventually(p.Ready()).Should(BeClosed())
		})

		It("runs all substeps in order and returns nil", func() {
			Eventually(testRunner1.RunCallCount).Should(Equal(1))
			go testRunner1.TriggerExit(nil)
			Eventually(testRunner2.RunCallCount).Should(Equal(1))
			go testRunner2.TriggerExit(nil)
			Eventually(testRunner3.RunCallCount).Should(Equal(1))
			go testRunner3.TriggerExit(nil)

			Eventually(p.Wait()).Should(Receive(BeNil()))
		})

		Context("when an step fails in the middle", func() {
			It("returns the error and does not continue performing", func() {
				disaster := errors.New("oh no!")

				Eventually(testRunner1.RunCallCount).Should(Equal(1))
				go testRunner1.TriggerExit(nil)
				Eventually(testRunner2.RunCallCount).Should(Equal(1))
				go testRunner2.TriggerExit(disaster)
				Consistently(testRunner3.RunCallCount).Should(Equal(0))

				Eventually(p.Wait()).Should(Receive(Equal(disaster)))
			})
		})
	})

	Describe("Signal", func() {
		It("cancels the currently running substep", func() {
			Eventually(testRunner1.RunCallCount).Should(Equal(1))
			go testRunner1.TriggerExit(nil)
			signalsChan := testRunner2.WaitForCall()
			p.Signal(os.Interrupt)
			Eventually(signalsChan).Should(Receive())
			Consistently(testRunner3.RunCallCount).Should(Equal(0))

			Eventually(p.Wait()).Should(Receive(Equal(steps.ErrCancelled)))
		})
	})
})
