package monitor_step_test

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestMonitorStep(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "MonitorStep Suite")
}
