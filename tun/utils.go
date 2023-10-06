package libtun

import (
	"log"
	"os"
	"os/exec"
)

func ExecCmd(c string, args ...string) error {
	log.Printf("exec cmd: %v %v:", c, args)
	cmd := exec.Command(c, args...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	cmd.Stdin = os.Stdin
	return cmd.Run()

}
