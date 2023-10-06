package libtun

func AddRouteToDev(cidr string, dev string) error {
	return ExecCmd("ip", "route", "add", cidr, "dev", dev)
}
