package runoncehandler_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"

	"github.com/cloudfoundry/gosteno"
)

func TestRunOnceHandler(t *testing.T) {
	RegisterFailHandler(Fail)
	gosteno.EnterTestMode()
	RunSpecs(t, "RunOnceHandler Suite")
}
