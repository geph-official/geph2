package main

import (
	"net"
)

type commandDialer interface {
	DialCmd(cmds ...string) (conn net.Conn, underlying interface{}, ok bool)
}
