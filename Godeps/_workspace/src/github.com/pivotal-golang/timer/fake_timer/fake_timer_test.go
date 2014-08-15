package fake_timer_test

import (
	"time"
	. "github.com/pivotal-golang/timer/fake_timer"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("FakeTimer", func() {
	var (
		timer       *FakeTimer
		initialTime time.Time
		Δ           time.Duration = 10 * time.Millisecond
	)

	BeforeEach(func() {
		initialTime = time.Date(2014, 1, 1, 3, 0, 30, 0, time.UTC)
		timer = NewFakeTimer(initialTime)
	})

	Describe("After", func() {
		It("returns a channel that receives after the given interval has elapsed", func() {
			timeChan := timer.After(10 * time.Second)
			Consistently(timeChan, Δ).ShouldNot(Receive())

			timer.Elapse(5 * time.Second)
			Consistently(timeChan, Δ).ShouldNot(Receive())

			timer.Elapse(4 * time.Second)
			Consistently(timeChan, Δ).ShouldNot(Receive())

			timer.Elapse(1 * time.Second)
			Eventually(timeChan).Should(Receive(Equal(initialTime.Add(10 * time.Second))))

			timer.Elapse(10 * time.Second)
			Consistently(timeChan, Δ).ShouldNot(Receive())
		})
	})

	Describe("Sleep", func() {
		It("blocks until the given interval elapses", func() {
			doneSleeping := make(chan struct{})
			go func() {
				timer.Sleep(10 * time.Second)
				close(doneSleeping)
			}()

			Consistently(doneSleeping, Δ).ShouldNot(BeClosed())

			timer.Elapse(5 * time.Second)
			Consistently(doneSleeping, Δ).ShouldNot(BeClosed())

			timer.Elapse(4 * time.Second)
			Consistently(doneSleeping, Δ).ShouldNot(BeClosed())

			timer.Elapse(1 * time.Second)
			Eventually(doneSleeping).Should(BeClosed())
		})
	})

	Describe("Every", func() {
		It("returns a channel that receives every time the given interval elapses", func() {
			timeChan := timer.Every(10 * time.Second)
			Consistently(timeChan, Δ).ShouldNot(Receive())

			timer.Elapse(5 * time.Second)
			Consistently(timeChan, Δ).ShouldNot(Receive())

			timer.Elapse(4 * time.Second)
			Consistently(timeChan, Δ).ShouldNot(Receive())

			timer.Elapse(1 * time.Second)
			Eventually(timeChan).Should(Receive(Equal(initialTime.Add(10 * time.Second))))

			timer.Elapse(10 * time.Second)
			Eventually(timeChan).Should(Receive(Equal(initialTime.Add(20 * time.Second))))

			timer.Elapse(10 * time.Second)
			Eventually(timeChan).Should(Receive(Equal(initialTime.Add(30 * time.Second))))
		})
	})
})
