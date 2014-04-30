package file_cache_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestFileCache(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "FileCache Suite")
}
