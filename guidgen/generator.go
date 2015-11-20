package guidgen

import (
	"github.com/nu7hatch/gouuid"
	"github.com/pivotal-golang/lager"
)

var DefaultGenerator Generator = &generator{}

//go:generate counterfeiter -o fakeguidgen/fake_generator.go . Generator

type Generator interface {
	Guid(lager.Logger) string
}

type generator struct{}

func (*generator) Guid(logger lager.Logger) string {
	guid, err := uuid.NewV4()
	if err != nil {
		logger.Fatal("failed-to-generate-guid", err)
	}
	return guid.String()
}
