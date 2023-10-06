package libtun

import (
	"io"
)

type Config struct {
	// MTU
	MTU int
	// TUN CIDR
	CIDR string
	// TUN Device Name
	Name string
}

type Interface struct {
	Name string
	MUT  int
	io.ReadWriteCloser
}

func (i *Interface) RouteAdd(cidr string) error {
	return AddRouteToDev(cidr, i.Name)
}
