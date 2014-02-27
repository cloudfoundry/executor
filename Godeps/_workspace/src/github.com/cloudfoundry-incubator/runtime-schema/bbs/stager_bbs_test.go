package bbs_test

import (
	. "github.com/cloudfoundry-incubator/runtime-schema/bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/storeadapter"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"time"
)

var _ = Describe("Stager BBS", func() {
	var bbs *BBS
	var runOnce models.RunOnce

	BeforeEach(func() {
		bbs = New(store)
		runOnce = models.RunOnce{
			Guid:            "some-guid",
			ExecutorID:      "executor-id",
			ContainerHandle: "container-handle",
		}
	})

	itRetriesUntilStoreComesBack := func(action func(*BBS, models.RunOnce) error) {
		It("should keep trying until the store comes back", func(done Done) {
			etcdRunner.GoAway()

			runResult := make(chan error)
			go func() {
				err := action(bbs, runOnce)
				runResult <- err
			}()

			time.Sleep(200 * time.Millisecond)

			etcdRunner.ComeBack()

			Ω(<-runResult).ShouldNot(HaveOccurred())

			close(done)
		}, 5)
	}

	Describe("DesireRunOnce", func() {
		Context("when the RunOnce has a CreatedAt time", func() {
			BeforeEach(func() {
				runOnce.CreatedAt = 1234812
				err := bbs.DesireRunOnce(runOnce)
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("creates /run_once/pending/<guid>", func() {
				node, err := store.Get("/v1/run_once/pending/some-guid")
				Ω(err).ShouldNot(HaveOccurred())
				Ω(node.Value).Should(Equal(runOnce.ToJSON()))
			})
		})

		Context("when the RunOnce has no CreatedAt time", func() {
			BeforeEach(func() {
				err := bbs.DesireRunOnce(runOnce)
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("creates /run_once/pending/<guid> and sets set the CreatedAt time to now", func() {
				runOnces, err := bbs.GetAllPendingRunOnces()
				Ω(err).ShouldNot(HaveOccurred())

				Ω(runOnces[0].CreatedAt).Should(BeNumerically("<=", time.Now().UnixNano()))
				Ω(runOnces[0].CreatedAt).Should(BeNumerically(">", time.Now().UnixNano()-int64(time.Second)))
			})
		})

		Context("Common cases", func() {
			BeforeEach(func() {
				runOnce.CreatedAt = 1234812
				err := bbs.DesireRunOnce(runOnce)
				Ω(err).ShouldNot(HaveOccurred())
			})

			Context("when the RunOnce is already pending", func() {
				It("should happily overwrite the existing RunOnce", func() {
					err := bbs.DesireRunOnce(runOnce)
					Ω(err).ShouldNot(HaveOccurred())
				})
			})

			Context("when the store is out of commission", func() {
				itRetriesUntilStoreComesBack((*BBS).DesireRunOnce)
			})
		})
	})

	Describe("ResolveRunOnce", func() {
		BeforeEach(func() {
			err := bbs.DesireRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())
		})

		It("should remove /run_once/pending/<guid>", func() {
			err := bbs.ResolveRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())

			_, err = store.Get("/v1/run_once/pending/some-guid")
			Ω(err).Should(Equal(storeadapter.ErrorKeyNotFound))
		})

		Context("when the store is out of commission", func() {
			itRetriesUntilStoreComesBack((*BBS).ResolveRunOnce)
		})
	})

	Describe("WatchForCompletedRunOnce", func() {
		var (
			events <-chan (models.RunOnce)
			stop   chan<- bool
		)

		BeforeEach(func() {
			events, stop, _ = bbs.WatchForCompletedRunOnce()
		})

		It("should send an event down the pipe for creates", func(done Done) {
			err := bbs.CompleteRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())

			Expect(<-events).To(Equal(runOnce))

			close(done)
		})

		It("should send an event down the pipe for sets", func(done Done) {
			err := bbs.DesireRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())

			err = bbs.CompleteRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())

			Expect(<-events).To(Equal(runOnce))

			bbs.ConvergeRunOnce(time.Second) //should bump the completed key

			Expect(<-events).To(Equal(runOnce))

			close(done)
		})

		It("should not send an event down the pipe for deletes", func(done Done) {
			err := bbs.CompleteRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())

			Expect(<-events).To(Equal(runOnce))

			bbs.ConvergeRunOnce(time.Second) //should delete the key

			otherRunOnce := runOnce
			otherRunOnce.Guid = runOnce.Guid + "1"

			err = bbs.CompleteRunOnce(otherRunOnce)
			Ω(err).ShouldNot(HaveOccurred())

			Expect(<-events).To(Equal(otherRunOnce))

			close(done)
		})

		It("closes the events channel when told to stop", func(done Done) {
			stop <- true

			err := bbs.CompleteRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())

			_, ok := <-events

			Expect(ok).To(BeFalse())

			close(done)
		})
	})
})
