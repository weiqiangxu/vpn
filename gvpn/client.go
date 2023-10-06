package main

import (
	"flag"
	"fmt"
	"github.com/zs-java/libgo-common/assert"
	"github.com/zs-java/libgo-common/nets"
	"github.com/zs-java/libgo-common/nets/handshake"
	"github.com/zs-java/libgo-common/nets/packet"
	"github.com/zs-java/libgo-common/nets/transaction"
	"gvpn/libgvpn"
	"net"
	"os"
)

func main() {
	cidr := flag.String("cidr", "182.1.1.101/24", "vlan cidr")
	serverAddress := flag.String("server", "127.0.0.1:51005", "server address")

	flag.Parse()

	ip, _, err := net.ParseCIDR(*cidr)
	assert.NotNil(err)

	storage := &libgvpn.ClientStorage{
		IP:   ip,
		CIDR: *cidr,
		Tun:  nil,
	}

	conn, err := nets.Dial("tcp", *serverAddress, func(conn net.Conn) *nets.Conn {
		return &nets.Conn{
			Conn:               conn,
			HandshakeHandler:   handshake.NewVersionClientHandler(libgvpn.Version),
			Reader:             packet.NewReader(conn),
			Writer:             packet.NewWriter(conn),
			TransactionManager: transaction.NewManager(),
			PacketHandlers: []*nets.PacketHandler{
				nets.DefaultTransactionPacketHandler,
				libgvpn.NewClientProxyPacketHandler(storage),
			},
			ExceptionHandler: func(ctx *nets.Context, err error) {
				fmt.Println(err)
				os.Exit(1)
			},
		}
	})
	assert.NotNil(err)

	// handshake
	assert.NotNil(conn.Handshake())

	// handle packet
	go conn.HandlePacket()

	bypass, err := libgvpn.RegisterClientAndGetBypass(conn, storage)
	assert.NotNil(err)
	fmt.Printf("Server Bypass: %s\n", bypass)

	libgvpn.SetupTunAndProxyToServer(conn, storage, bypass)
}
