package linuxplugin

import (
	"fmt"

	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

type LinuxPlugin struct{}

func New() *LinuxPlugin {
	return &LinuxPlugin{}
}

func (p LinuxPlugin) BuildRunScript(run models.RunAction) string {
	script := ""

	for key, val := range run.Env {
		// naively assumes Go's string quotes are compatible with Bash,
		// which it's not. See http://golang.org/ref/spec#String_literals
		script += fmt.Sprintf("export %s=%q\n", key, val)
	}

	script += run.Script

	return script
}

func (p LinuxPlugin) BuildCreateDirectoryRecursivelyCommand(path string) string {
	return fmt.Sprintf("mkdir -p %s", path)
}
