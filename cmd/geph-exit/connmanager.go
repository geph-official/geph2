package main

import (
	"net"
	"time"

	lru "github.com/hashicorp/golang-lru"
	log "github.com/sirupsen/logrus"
	"golang.org/x/time/rate"
)

var reportRL = rate.NewLimiter(20, 10)

var connlru, _ = lru.NewWithEvict(4096, func(k, v interface{}) {
	if statClient != nil && reportRL.Allow() {
		statClient.Timing(hostname+".lruLifetime", time.Since(v.(time.Time)).Milliseconds())
	}
	log.Println("lasted", time.Since(v.(time.Time)))
	k.(net.Conn).Close()
})

func regConn(conn net.Conn) {
	connlru.Add(conn, time.Now())
}
