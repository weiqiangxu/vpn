package libtun

import (
	"github.com/songgao/water"
	"github.com/zs-java/libgo-common/utils"
	"net"
	"strconv"
)

type LinuxTun struct {
	config Config
	device *water.Interface
}

func (c *LinuxTun) Read(p []byte) (n int, err error) {
	return c.device.Read(p)
}

func (c *LinuxTun) Write(p []byte) (n int, err error) {
	return c.device.Write(p)
}

func (c *LinuxTun) Close() error {
	return c.device.Close()
}

func (c *LinuxTun) startUp() error {
	_, ipNet, err := net.ParseCIDR(c.config.CIDR)
	if err != nil {
		// log.Fatalln("parse CIDR error", err)
		return err
	}
	ipNet.IP[3]++

	utils.ExecCmd("/sbin/ip", "link", "set", "dev", c.device.Name(), "mtu", strconv.Itoa(c.config.MTU))
	utils.ExecCmd("/sbin/ip", "addr", "add", c.config.CIDR, "dev", c.device.Name())
	utils.ExecCmd("/sbin/ip", "link", "set", "dev", c.device.Name(), "up")
	return nil
}

func NewTun(config Config) (*Interface, error) {
	tunConfig := water.Config{
		DeviceType: water.TUN,
	}
	dev, err := water.New(tunConfig)
	if err != nil {
		// log.Fatalln("create tun error", err)
		return nil, err
	}
	linuxTun := &LinuxTun{
		config: config,
		device: dev,
	}
	err = linuxTun.startUp()
	if err != nil {
		return nil, err
	}
	return &Interface{
		Name:            dev.Name(),
		MUT:             config.MTU,
		ReadWriteCloser: linuxTun,
	}, nil

}
