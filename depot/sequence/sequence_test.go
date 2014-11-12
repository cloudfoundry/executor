package sequence_test

import (
	"errors"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	. "github.com/cloudfoundry-incubator/executor/depot/sequence"
	"github.com/cloudfoundry-incubator/executor/depot/sequence/fake_step"
)

var _ = Describe("Sequence", func() {
	It("performs them all in order and sends back nil", func(done Done) {
		defer close(done)

		seq := make(chan int, 3)

		sequence := New([]Step{
			&fake_step.FakeStep{
				PerformStub: func() error {
					seq <- 1
					return nil
				},
			},
			&fake_step.FakeStep{
				PerformStub: func() error {
					seq <- 2
					return nil
				},
			},
			&fake_step.FakeStep{
				PerformStub: func() error {
					seq <- 3
					return nil
				},
			},
		})

		result := make(chan error)
		go func() { result <- sequence.Perform() }()

		Ω(<-seq).Should(Equal(1))
		Ω(<-seq).Should(Equal(2))
		Ω(<-seq).Should(Equal(3))

		Ω(<-result).Should(BeNil())
	})

	Context("when an step fails in the middle", func() {
		It("sends back the error and does not continue performing", func(done Done) {
			defer close(done)

			disaster := errors.New("oh no!")

			seq := make(chan int, 3)

			sequence := New([]Step{
				&fake_step.FakeStep{
					PerformStub: func() error {
						seq <- 1
						return nil
					},
				},
				&fake_step.FakeStep{
					PerformStub: func() error {
						return disaster
					},
				},
				&fake_step.FakeStep{
					PerformStub: func() error {
						seq <- 3
						return nil
					},
				},
			})

			result := make(chan error)
			go func() { result <- sequence.Perform() }()

			Ω(<-seq).Should(Equal(1))

			Ω(<-result).Should(Equal(disaster))

			Consistently(seq).ShouldNot(Receive())
		})
	})

	Context("when the sequence is cancelled in the middle", func() {
		It("cancels the running step", func(done Done) {
			defer close(done)

			seq := make(chan int, 3)

			waitingForInterrupt := make(chan bool)
			interrupt := make(chan bool)
			interrupted := make(chan bool)

			sequence := New([]Step{
				&fake_step.FakeStep{
					PerformStub: func() error {
						seq <- 1
						return nil
					},
				},
				&fake_step.FakeStep{
					PerformStub: func() error {
						seq <- 2

						waitingForInterrupt <- true
						<-interrupt
						interrupted <- true

						return nil
					},
					CancelStub: func() {
						interrupt <- true
					},
				},
				&fake_step.FakeStep{
					PerformStub: func() error {
						seq <- 3
						return nil
					},
				},
			})

			result := make(chan error)
			go func() { result <- sequence.Perform() }()

			Ω(<-seq).Should(Equal(1))
			Ω(<-seq).Should(Equal(2))

			<-waitingForInterrupt

			sequence.Cancel()

			<-interrupted

			Ω(<-result).Should(Equal(CancelledError))

			Consistently(seq).ShouldNot(Receive())
		})
	})
})
