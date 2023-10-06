package libgvpn

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/mariomac/gostream/item"
	"github.com/mariomac/gostream/stream"
	"github.com/songgao/water/waterutil"
	"github.com/zs-java/libgo-common/assert"
	"github.com/zs-java/libgo-common/nets"
	"github.com/zs-java/libgo-common/nets/libtun"
	"github.com/zs-java/libgo-common/nets/packet"
	"io"
	"net"
)

func NewServerAcceptClientRegisterCidrHandler(storage *ServerStorage) *nets.PacketHandler {
	return &nets.PacketHandler{
		Name: "ServerAcceptClientRegisterCidrHandler",
		Cmd:  CmdClientRegisterCidr,
		Action: func(ctx *nets.Context, p *packet.Packet) error {
			// 获取客户单发送的 cidr
			var ip net.IP = p.Data
			fmt.Printf("client register ip: %s\n", ip.String())

			result := ClientRegisterResult{}

			// 判断当前连接是否已经注册过信息
			if _, ok := storage.ConnMap[ctx.Conn]; !ok {
				// 没注册过，检查 cidr 是否已被注册
				for _, info := range storage.ConnMap {
					if info.IP.Equal(ip) {
						// 注册失败
						result.Success = false
						result.Message = "CIDR is registered!"
						_ = ctx.WritePacket(ctx.TransactionManager.CreateCallbackPacket(p.TransactionId, assert.GetFirstByteArr(json.Marshal(result))))
						_ = ctx.Close()
						return nil
					}
				}
			}
			storage.ConnMap[ctx.Conn] = &ClientInfo{IP: ip}
			result.Success = true
			result.Bypass = storage.Bypass
			return ctx.WritePacket(ctx.TransactionManager.CreateCallbackPacket(p.TransactionId, assert.GetFirstByteArr(json.Marshal(result))))
		},
	}
}

func NewServerProxyPacketHandler(storage *ServerStorage) *nets.PacketHandler {
	return &nets.PacketHandler{
		Name: "ServerProxyPacketHandler",
		Cmd:  CmdProxyPacket,
		Action: func(ctx *nets.Context, p *packet.Packet) error {
			// 目标 ip
			destIpv4 := waterutil.IPv4Destination(p.Data)
			// 判断目标 ip 是否在 旁路网段内
			if !IpContains(storage.Bypass, destIpv4) {
				return nil
			}
			// 写入 tun 网卡
			if storage.Tun == nil {
				return nil
			}
			fmt.Printf("client: srcIpv4=%s, destIpv4=%s\n", waterutil.IPv4Source(p.Data), waterutil.IPv4Destination(p.Data))
			_, err := storage.Tun.Write(p.Data)
			return err
		},
	}
}

func NewClientProxyPacketHandler(storage *ClientStorage) *nets.PacketHandler {
	return &nets.PacketHandler{
		Name: "ClientProxyPacketHandler",
		Cmd:  CmdProxyPacket,
		Action: func(ctx *nets.Context, p *packet.Packet) error {
			fmt.Println("tun", storage.Tun)
			if storage.Tun == nil {
				// tun 网卡为准备好，忽略数据包
				return nil
			}
			fmt.Println(p.Data)
			fmt.Printf("server: srcIpv4=%s, destIpv4=%s\n", waterutil.IPv4Source(p.Data).String(), waterutil.IPv4Destination(p.Data).String())
			// 将服务端转发回来的数据包写入 tun 网卡
			_, err := storage.Tun.Write(p.Data)
			return err
		},
	}
}

func IpContains(cidr string, ip net.IP) bool {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return false
	}
	return ipnet.Contains(ip)
}

func SetupTunAndProxyToClient(storage *ServerStorage) {
	// setup tun
	tun, err := libtun.NewTun(libtun.Config{
		MTU:  1500,
		CIDR: storage.CIDR,
		Name: "gvpn",
	})
	assert.NotNil(err)
	storage.Tun = tun
	defer func() {
		if err := recover(); err != nil {
			fmt.Printf("tun device read err:%v\n", err)
		}
		_ = tun.Close()
		fmt.Println("tun dev closed")
	}()

	buf := make([]byte, tun.MUT)
	for {
		n, err := tun.Read(buf)
		if err != nil {
			panic(err)
		}
		data := buf[:n]
		if !waterutil.IsIPv4(data) {
			continue
		}
		// 目标IP
		destIpv4 := waterutil.IPv4Destination(data)
		// 判断目标IP是否属于虚拟网卡网段
		if !IpContains(storage.CIDR, destIpv4) {
			continue
		}
		// 找到对应的连接并写入数据
		if conn, ok := stream.Map(stream.OfMap(storage.ConnMap).Filter(func(entry item.Pair[*nets.Conn, *ClientInfo]) bool {
			return destIpv4.Equal(entry.Val.IP)
		}), func(entry item.Pair[*nets.Conn, *ClientInfo]) *nets.Conn {
			return entry.Key
		}).FindFirst(); ok {
			_ = conn.WritePacket(packet.NewPacket(CmdProxyPacket, data))
		}
		// 没有找到对应的客户端连接
	}
}

func SetupTunAndProxyToServer(conn *nets.Conn, storage *ClientStorage, bypass string) {
	// 创建 tun 虚拟网络设备，设置旁路路由到 tun 网卡
	tun, err := libtun.NewTun(libtun.Config{
		MTU:  1500,
		CIDR: storage.CIDR,
		Name: "gvpn",
	})
	assert.NotNil(err)
	storage.Tun = tun

	defer func() {
		if err := recover(); err != nil {
			fmt.Printf("tun device read err:%v\n", err)
		}
		_ = tun.Close()
		fmt.Println("tun dev closed")
	}()

	// 添加旁路路由到 tun 网卡
	assert.NotNil(tun.RouteAdd(bypass))

	buf := make([]byte, tun.MUT)
	for {
		n, err := tun.Read(buf)
		if err != nil {
			panic(err)
		}
		// IP 数据包
		data := buf[:n]
		if !waterutil.IsIPv4(data) {
			// 只处理 ipv4 数据包
			continue
		}
		// 目标IP
		destIp := waterutil.IPv4Destination(data)
		// 如果目标IP与本地 Tun 设备的IP相同，则将数据包写回到 Tun 设备
		if destIp.Equal(storage.IP) {
			_, _ = tun.Write(data)
			continue
		}
		// 发送到 server
		_ = conn.WritePacket(packet.NewPacket(CmdProxyPacket, data))
	}
}

func RegisterClientAndGetBypass(conn *nets.Conn, storage *ClientStorage) (string, error) {
	t := conn.TransactionManager.CreateTransaction(CmdClientRegisterCidr, storage.IP)
	assert.NotNil(conn.WritePacket(t.GetPacket()))
	assert.NotNil(t.Wait())
	var result ClientRegisterResult
	assert.NotNil(json.Unmarshal(t.GetCallbackPacket().Data, &result))
	if !result.Success {
		return "", errors.New(fmt.Sprintf("Register CIDR(%s) Failed: %s\n", storage.CIDR, result.Message))
	}
	fmt.Printf("Register CIDR(%s) Successful!\n", storage.CIDR)
	return result.Bypass, nil
}

var PrintEofExceptionHandler = func(ctx *nets.Context, err error) {
	if err != io.EOF {
		fmt.Println(err)
	} else {
		panic(err)
	}
}
