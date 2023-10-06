package libtun

import (
	"github.com/songgao/water"
	"net"
)

type DarwinTun struct {
	config Config
	device *water.Interface
}

func (c *DarwinTun) Read(p []byte) (n int, err error) {
	return c.device.Read(p)
}

func (c *DarwinTun) Write(p []byte) (n int, err error) {
	return c.device.Write(p)
}

func (c *DarwinTun) Close() error {
	return c.device.Close()
}

func (c *DarwinTun) startUp() error {
	ip, ipNet, err := net.ParseCIDR(c.config.CIDR)
	if err != nil {
		// log.Fatalln("parse CIDR error", err)
		return err
	}
	ipNet.IP[3]++

	// 启用隧道
	_ = ExecCmd("ifconfig", c.device.Name(), "inet", ip.String(), ipNet.IP.String(), "up")
	// 添加成员路由
	_ = ExecCmd("route", "add", c.config.CIDR, "-interface", c.device.Name())
	return nil
}

func NewTun(config Config) (*Interface, error) {
	tunConfig := water.Config{
		DeviceType: water.TUN,
	}
	dev, err := water.New(tunConfig)
	if err != nil {
		return nil, err
	}

	darwinTun := &DarwinTun{
		config: config,
		device: dev,
	}

	err = darwinTun.startUp()
	if err != nil {
		return nil, err
	}

	return &Interface{
		Name:            dev.Name(),
		MUT:             config.MTU,
		ReadWriteCloser: darwinTun,
	}, nil

}
