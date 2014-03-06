package run_once_transformer_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestRunOnceTransformer(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "RunOnceTransformer Suite")
}
