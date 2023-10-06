package libgvpn

import (
	"github.com/zs-java/libgo-common/nets"
	"github.com/zs-java/libgo-common/nets/libtun"
	"net"
)

const Version = 1.0

const (
	CmdClientRegisterCidr = iota + 1
	CmdProxyPacket
)

type ClientRegisterResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Bypass  string `json:"bypass"`
}

type ServerStorage struct {
	CIDR    string
	Bypass  string
	Tun     *libtun.Interface
	ConnMap map[*nets.Conn]*ClientInfo
}

type ClientInfo struct {
	IP net.IP
}

type ClientStorage struct {
	IP   net.IP
	CIDR string
	Tun  *libtun.Interface
}
