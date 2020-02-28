package main

import (
	"net"

	lru "github.com/hashicorp/golang-lru"
)

var connlru, _ = lru.NewWithEvict(4096, func(k, v interface{}) {
	//log.Println("LRU closing", k.(net.Conn).RemoteAddr())
	k.(net.Conn).Close()
})

func regConn(conn net.Conn) {
	connlru.Add(conn, nil)
}
