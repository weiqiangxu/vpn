package libtun

func AddRouteToDev(cidr string, dev string) error {
	return ExecCmd("route", "add", cidr, "-interface", dev)
}
