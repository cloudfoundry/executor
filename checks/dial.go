package checks

import (
	"net"
	"time"
)

type dial struct {
	Network string `json:"network"`
	Addr    string `json:"addr"`
}

func NewDial(network, addr string) Check {
	return &dial{
		Network: network,
		Addr:    addr,
	}
}

func (dial *dial) Check() bool {
	_, err := net.DialTimeout(dial.Network, dial.Addr, time.Second)
	return err == nil
}
