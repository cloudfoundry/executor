package backend_plugin

import (
	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

type BackendPlugin interface {
	BuildRunScript(models.RunAction) string
}
