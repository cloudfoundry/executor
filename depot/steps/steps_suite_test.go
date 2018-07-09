package steps_test

import (
	"context"
	"fmt"
	"time"

	"github.com/fortytw2/leaktest"
	multierror "github.com/hashicorp/go-multierror"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

var (
	ctx                     context.Context
	checkGoroutines, cancel func()
	goroutineErrors         *multierror.Error
)

type errorReporter struct {
}

func (errorReporter) Errorf(format string, args ...interface{}) {
	multierror.Append(goroutineErrors, fmt.Errorf(format, args...))
}

func TestSteps(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Steps Suite")
}

var _ = BeforeEach(func() {
	goroutineErrors = &multierror.Error{}
	ctx, cancel = context.WithCancel(context.Background())
	checkGoroutines = leaktest.CheckContext(ctx, errorReporter{})
})

var _ = AfterEach(func() {
	if checkGoroutines != nil {
		time.AfterFunc(time.Second, cancel)
		checkGoroutines()
	}

	if err := goroutineErrors.ErrorOrNil(); err != nil {
		Fail(err.Error())
	}
})
