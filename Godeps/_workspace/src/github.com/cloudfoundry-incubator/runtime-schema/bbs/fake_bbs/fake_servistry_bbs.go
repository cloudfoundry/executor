package fake_bbs

import (
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"time"
)

type RegisterCCInputs struct {
	Registration models.CCRegistrationMessage
	Ttl          time.Duration
}

type FakeServistryBBS struct {
	RegisterCCInputs  chan RegisterCCInputs
	RegisterCCOutputs chan error

	UnregisterCCInputs  chan models.CCRegistrationMessage
	UnregisterCCOutputs chan error
}

func NewFakeServistryBBS() *FakeServistryBBS {
	return &FakeServistryBBS{
		RegisterCCInputs:    make(chan RegisterCCInputs),
		RegisterCCOutputs:   make(chan error),
		UnregisterCCInputs:  make(chan models.CCRegistrationMessage),
		UnregisterCCOutputs: make(chan error),
	}
}

func (bbs *FakeServistryBBS) RegisterCC(registration models.CCRegistrationMessage, ttl time.Duration) error {
	bbs.RegisterCCInputs <- RegisterCCInputs{
		Registration: registration,
		Ttl:          ttl,
	}

	return <-bbs.RegisterCCOutputs
}

func (bbs *FakeServistryBBS) UnregisterCC(registration models.CCRegistrationMessage) error {
	bbs.UnregisterCCInputs <- registration
	return <-bbs.UnregisterCCOutputs
}
