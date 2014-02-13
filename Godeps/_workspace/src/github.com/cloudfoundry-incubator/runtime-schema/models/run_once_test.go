package models_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	. "github.com/cloudfoundry-incubator/runtime-schema/models"
)

var _ = Describe("RunOnce", func() {
	var runOnce RunOnce

	runOncePayload := `{
		"guid":"some-guid",
		"reply_to":"some-requester",
		"stack":"some-stack",
		"executor_id":"executor",
		"actions":[
			{
				"action":"download",
				"args":{"from":"old_location","to":"new_location","extract":true}
			}
		],
		"container_handle":"17fgsafdfcvc",
		"failed":true,
		"failure_reason":"because i said so",
		"memory_mb":256,
		"disk_mb":1024,
		"log": {
			"guid": "123",
			"source_name": "APP",
			"index": 42
		}
	}`

	BeforeEach(func() {
		index := 42

		runOnce = RunOnce{
			Guid:    "some-guid",
			ReplyTo: "some-requester",
			Stack:   "some-stack",
			Actions: []ExecutorAction{
				{
					Action: DownloadAction{
						From:    "old_location",
						To:      "new_location",
						Extract: true,
					},
				},
			},
			Log: LogConfig{
				Guid:       "123",
				SourceName: "APP",
				Index:      &index,
			},
			ExecutorID:      "executor",
			ContainerHandle: "17fgsafdfcvc",
			Failed:          true,
			FailureReason:   "because i said so",
			MemoryMB:        256,
			DiskMB:          1024,
		}
	})

	Describe("ToJSON", func() {
		It("should JSONify", func() {
			json := runOnce.ToJSON()
			Ω(string(json)).Should(MatchJSON(runOncePayload))
		})
	})

	Describe("NewRunOnceFromJSON", func() {
		It("returns a RunOnce with correct fields", func() {
			decodedRunOnce, err := NewRunOnceFromJSON([]byte(runOncePayload))
			Ω(err).ShouldNot(HaveOccurred())

			Ω(decodedRunOnce).Should(Equal(runOnce))
		})

		Context("with an invalid payload", func() {
			It("returns the error", func() {
				decodedRunOnce, err := NewRunOnceFromJSON([]byte("butts lol"))
				Ω(err).Should(HaveOccurred())

				Ω(decodedRunOnce).Should(BeZero())
			})
		})
	})
})
