package fetch_result_step_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestFetchResultStep(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Fetch_result_step Suite")
}
