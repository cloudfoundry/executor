package actionrunner_test

import (
	. "github.com/cloudfoundry-incubator/executor/actionrunner"
	"github.com/cloudfoundry-incubator/executor/actionrunner/downloader/fakedownloader"
	"github.com/cloudfoundry-incubator/executor/actionrunner/uploader/fakeuploader"
	"github.com/cloudfoundry-incubator/executor/linuxplugin"
	steno "github.com/cloudfoundry/gosteno"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/vito/gordon/fake_gordon"
	"os"

	"testing"
)

func TestActionrunner(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Actionrunner Suite")
}

var (
	runner      *ActionRunner
	downloader  *fakedownloader.FakeDownloader
	uploader    *fakeuploader.FakeUploader
	gordon      *fake_gordon.FakeGordon
	linuxPlugin *linuxplugin.LinuxPlugin
)

var _ = BeforeEach(func() {
	gordon = fake_gordon.New()
	downloader = &fakedownloader.FakeDownloader{}
	uploader = &fakeuploader.FakeUploader{}
	linuxPlugin = linuxplugin.New()
	runner = New(gordon, linuxPlugin, downloader, uploader, os.TempDir(), steno.NewLogger("test-logger"))
})
