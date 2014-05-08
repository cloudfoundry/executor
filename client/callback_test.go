package client_test

import (
	. "github.com/cloudfoundry-incubator/executor/client"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Callback", func() {
	It("ummarshals ContainerRunResult", func() {
		containerRunResultJSON := `
    {
      "failed" : true,
      "failure_reason" : "it failed",
      "result" : "the-result",
      "metadata" : "c29tZS1tZXRhZGF0YQ=="
    }`
		result, err := NewContainerRunResultFromJSON([]byte(containerRunResultJSON))
		Ω(err).ShouldNot(HaveOccurred())
		Ω(result).Should(Equal(ContainerRunResult{
			Failed:        true,
			FailureReason: "it failed",
			Result:        "the-result",
			Metadata:      []byte("some-metadata"),
		}))
	})
})
