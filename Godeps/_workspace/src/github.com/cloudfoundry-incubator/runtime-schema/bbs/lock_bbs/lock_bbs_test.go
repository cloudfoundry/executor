package lock_bbs_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"time"

	. "github.com/cloudfoundry-incubator/runtime-schema/bbs/lock_bbs"
	"github.com/cloudfoundry/storeadapter/test_helpers"
)

type lockBBSTest interface {
	MaintainAuctioneerLock(interval time.Duration, auctioneerID string) (<-chan bool, chan<- chan bool, error)
	MaintainConvergeLock(interval time.Duration, convergerID string) (<-chan bool, chan<- chan bool, error)
}

var methodsToTest = map[string]func(bbs lockBBSTest, interval time.Duration, auctioneerID string) (<-chan bool, chan<- chan bool, error){
	"MaintainAuctioneerLock": lockBBSTest.MaintainAuctioneerLock,
	"MaintainConvergeLock":   lockBBSTest.MaintainConvergeLock,
}

var _ = Describe("Lock BBS", func() {
	var bbs *LockBBS

	BeforeEach(func() {
		bbs = New(etcdClient)
	})

	for name, f := range methodsToTest {
		Context(name, func() {
			Context("when the lock is available", func() {
				It("should return immediately", func() {
					status, releaseLock, err := f(bbs, 1*time.Minute, "id")
					Ω(err).ShouldNot(HaveOccurred())

					defer close(releaseLock)

					Ω(status).ShouldNot(BeNil())
					Ω(releaseLock).ShouldNot(BeNil())

					reporter := test_helpers.NewStatusReporter(status)
					Eventually(reporter.Locked).Should(Equal(true))
				})

				It("should maintain the lock in the background", func() {
					interval := 1 * time.Second

					status, releaseLock, err := f(bbs, interval, "id_1")
					Ω(err).ShouldNot(HaveOccurred())

					defer close(releaseLock)

					reporter1 := test_helpers.NewStatusReporter(status)
					Eventually(reporter1.Locked).Should(BeTrue())

					status2, releaseLock2, err2 := f(bbs, interval, "id_2")
					Ω(err2).ShouldNot(HaveOccurred())

					defer close(releaseLock2)

					reporter2 := test_helpers.NewStatusReporter(status2)
					Consistently(reporter2.Locked, (interval * 2).Seconds()).Should(BeFalse())
				})

				Context("when the lock disappears after it has been acquired (e.g. ETCD store is reset)", func() {
					It("should send a notification down the lostLockChannel", func() {
						status, releaseLock, err := f(bbs, 1*time.Second, "id")
						Ω(err).ShouldNot(HaveOccurred())

						reporter := test_helpers.NewStatusReporter(status)

						Eventually(reporter.Locked).Should(BeTrue())

						etcdRunner.Stop()

						Eventually(reporter.Locked).Should(BeFalse())

						releaseLock <- nil
					})
				})
			})

			Context("when releasing the lock", func() {
				It("makes it available for others trying to acquire it", func() {
					interval := 1 * time.Second

					status, release, err := f(bbs, interval, "my_id")
					Ω(err).ShouldNot(HaveOccurred())

					reporter1 := test_helpers.NewStatusReporter(status)
					Eventually(reporter1.Locked, (interval * 2).Seconds()).Should(BeTrue())

					status2, release2, err2 := f(bbs, interval, "other_id")
					Ω(err2).ShouldNot(HaveOccurred())

					reporter2 := test_helpers.NewStatusReporter(status2)
					Consistently(reporter2.Locked, (interval * 2).Seconds()).Should(BeFalse())

					Ω(reporter1.Reporting()).Should(BeTrue())

					release <- nil

					Eventually(reporter1.Reporting).Should(BeFalse())

					Eventually(reporter2.Locked, (interval * 2).Seconds()).Should(BeTrue())

					release2 <- nil

					Eventually(reporter2.Reporting).Should(BeFalse())
				})
			})
		})
	}
})
