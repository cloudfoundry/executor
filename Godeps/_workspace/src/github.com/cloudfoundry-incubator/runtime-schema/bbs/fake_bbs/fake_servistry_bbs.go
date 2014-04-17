package fake_bbs

import (
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"time"
)

type RegisterCCInputs struct {
	Message models.CCRegistrationMessage
	Ttl     time.Duration
}

type UnregisterCCInputs struct {
	Message models.CCRegistrationMessage
}

type FakeServistryBBS struct {
	RegisterCCInputs  []RegisterCCInputs
	RegisterCCOutputs struct {
		Err error
	}

	UnregisterCCInputs  []UnregisterCCInputs
	UnregisterCCOutputs struct {
		Err error
	}

	GetAvailableCCOutputs struct {
		Urls []string
		Err  error
	}
}

func NewFakeServistryBBS() *FakeServistryBBS {
	return &FakeServistryBBS{}
}

func (bbs *FakeServistryBBS) RegisterCC(msg models.CCRegistrationMessage, ttl time.Duration) error {
	bbs.RegisterCCInputs = append(bbs.RegisterCCInputs, RegisterCCInputs{
		Message: msg,
		Ttl:     ttl,
	})
	return bbs.RegisterCCOutputs.Err
}

func (bbs *FakeServistryBBS) UnregisterCC(msg models.CCRegistrationMessage) error {
	bbs.UnregisterCCInputs = append(bbs.UnregisterCCInputs, UnregisterCCInputs{
		Message: msg,
	})
	return bbs.UnregisterCCOutputs.Err
}

func (bbs *FakeServistryBBS) GetAvailableCC() ([]string, error) {
	return bbs.GetAvailableCCOutputs.Urls, bbs.GetAvailableCCOutputs.Err
}
