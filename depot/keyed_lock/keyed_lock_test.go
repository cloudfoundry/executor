package keyed_lock_test

import (
	"math"
	"runtime"
	"strconv"

	"github.com/cloudfoundry-incubator/executor/depot/keyed_lock"
	"github.com/pivotal-golang/lager/lagertest"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("KeyedLock", func() {
	var (
		lockManager keyed_lock.LockManager
		logger      *lagertest.TestLogger
	)

	BeforeEach(func() {
		lockManager = keyed_lock.NewLockManager()
		logger = lagertest.NewTestLogger("locks")
	})

	Describe("Lock", func() {
		Context("when the key hasn't previously been locked", func() {
			It("allows access", func() {
				accessGrantedCh := make(chan struct{})
				go func() {
					lockManager.Lock(logger, "the-key")
					close(accessGrantedCh)
				}()
				Eventually(accessGrantedCh).Should(BeClosed())
			})
		})

		Context("when the key is currently locked", func() {
			It("blocks until it is unlocked", func() {
				firstProcReadyCh := make(chan struct{})
				firstProcWaitCh := make(chan struct{})
				firstProcDoneCh := make(chan struct{})
				secondProcReadyCh := make(chan struct{})
				secondProcDoneCh := make(chan struct{})

				go func() {
					lockManager.Lock(logger, "the-key")
					close(firstProcReadyCh)
					<-firstProcWaitCh
					lockManager.Unlock(logger, "the-key")
					close(firstProcDoneCh)
				}()

				Eventually(firstProcReadyCh).Should(BeClosed())

				go func() {
					lockManager.Lock(logger, "the-key")
					close(secondProcReadyCh)
					lockManager.Unlock(logger, "the-key")
					close(secondProcDoneCh)
				}()

				Consistently(secondProcReadyCh).ShouldNot(BeClosed())
				firstProcWaitCh <- struct{}{}
				Eventually(secondProcDoneCh).Should(BeClosed())
			})
		})
	})

	Describe("Unlock", func() {
		Context("when the key has not been locked", func() {
			It("panics", func() {
				Expect(func() {
					lockManager.Unlock(logger, "key")
				}).To(

					Panic())

			})
		})
	})

	Describe("Reap unused locks", func() {
		It("does not leak", func() {
			var beforeStats, afterStats runtime.MemStats

			runtime.GC()
			runtime.ReadMemStats(&beforeStats)

			for i := 0; i < 10000; i++ {
				k := strconv.Itoa(i)
				lockManager.Lock(logger, k)
				lockManager.Unlock(logger, k)
			}

			runtime.GC()
			runtime.ReadMemStats(&afterStats)

			Expect(math.Abs(float64(afterStats.HeapObjects) - float64(beforeStats.HeapObjects))).To(BeNumerically("<", 10000))
		})
	})
})
