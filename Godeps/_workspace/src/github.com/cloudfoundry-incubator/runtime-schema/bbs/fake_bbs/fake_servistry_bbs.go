package fake_bbs

import (
	"time"
)

type RegisterCCInputs struct {
	Host string
	Port int
	Ttl  time.Duration
}

type UnregisterCCInputs struct {
	Host string
	Port int
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

func (bbs *FakeServistryBBS) RegisterCC(host string, port int, ttl time.Duration) error {
	bbs.RegisterCCInputs = append(bbs.RegisterCCInputs, RegisterCCInputs{
		Host: host,
		Port: port,
		Ttl:  ttl,
	})
	return bbs.RegisterCCOutputs.Err
}

func (bbs *FakeServistryBBS) UnregisterCC(host string, port int) error {
	bbs.UnregisterCCInputs = append(bbs.UnregisterCCInputs, UnregisterCCInputs{
		Host: host,
		Port: port,
	})
	return bbs.UnregisterCCOutputs.Err
}

func (bbs *FakeServistryBBS) GetAvailableCC() ([]string, error) {
	return bbs.GetAvailableCCOutputs.Urls, bbs.GetAvailableCCOutputs.Err
}
