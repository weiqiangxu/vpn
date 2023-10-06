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
)

func main() {
	serverAddress := flag.String("address", ":51005", "server address")
	cidr := flag.String("cidr", "182.1.1.0/24", "vlan cidr")
	bypass := flag.String("bypass", "172.17.0.0/16", "bypass cidr")

	flag.Parse()

	storage := &libgvpn.ServerStorage{
		ConnMap: make(map[*nets.Conn]*libgvpn.ClientInfo),
		Bypass:  *bypass,
		Tun:     nil,
		CIDR:    *cidr,
	}

	listen, err := nets.Listen("tcp", *serverAddress, func(conn net.Conn) *nets.Conn {
		return &nets.Conn{
			Conn:               conn,
			HandshakeHandler:   handshake.NewVersionServerHandler(libgvpn.Version),
			Reader:             packet.NewReader(conn),
			Writer:             packet.NewWriter(conn),
			TransactionManager: transaction.NewManager(),
			PacketHandlers: []*nets.PacketHandler{
				nets.DefaultTransactionPacketHandler,
				libgvpn.NewServerAcceptClientRegisterCidrHandler(storage),
				libgvpn.NewServerProxyPacketHandler(storage),
			},
			ExceptionHandler: libgvpn.PrintEofExceptionHandler,
			ClosedHandler: func(ctx *nets.Context) {
				delete(storage.ConnMap, ctx.Conn)
			},
		}
	})
	assert.NotNil(err)

	go libgvpn.SetupTunAndProxyToClient(storage)

	for {
		accept, err := listen.Accept()
		if err != nil {
			fmt.Println(err)
			continue
		}
		go func() {
			_ = accept.Handshake()
			accept.HandlePacket()
		}()
	}

}
