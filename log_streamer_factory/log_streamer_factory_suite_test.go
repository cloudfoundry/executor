package log_streamer_factory_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestLog_streamer_factory(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Log_streamer_factory Suite")
}
