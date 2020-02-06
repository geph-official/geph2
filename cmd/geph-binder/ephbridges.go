package main

import (
	"crypto/rand"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/geph-official/geph2/libs/cshirt2"
	"github.com/patrickmn/go-cache"
)

// functions for getting *ephemeral* bridge info

type ebMapKey struct {
	BridgeHost string
	Cookie     []byte
	Exit       string
}

func (ek ebMapKey) String() string {
	return fmt.Sprintf("%v:%X:%v", ek.BridgeHost, ek.Cookie, ek.Exit)
}

type ebMapVal struct {
	Bridge string
	Cookie []byte
}

var ebCache = cache.New(time.Minute*10, time.Minute)

func bridgeToEphBridge(bridgeHost string, bridgeCookie []byte, exitHost string) (ev ebMapVal, err error) {
	mapKeyStr := ebMapKey{bridgeHost, bridgeCookie, exitHost}.String()
	if evi, ok := ebCache.Get(mapKeyStr); ok {
		ev = evi.(ebMapVal)
		return
	}
	randCookie := make([]byte, 32)
	rand.Read(randCookie)
	// first make our connection
	rawConn, err := net.DialTimeout("tcp4", bridgeHost, time.Second*10)
	if err != nil {
		return
	}
	defer rawConn.Close()
	rawConn.SetDeadline(time.Now().Add(time.Minute))
	// then we negotiate
	csConn, err := cshirt2.Client(bridgeCookie, rawConn)
	if err != nil {
		return
	}
	// then we send
	rlp.Encode(csConn, "conn/e2e")
	rlp.Encode(csConn, exitHost)
	rlp.Encode(csConn, randCookie)
	// we receive the port
	var port uint
	err = rlp.Decode(csConn, &port)
	if err != nil {
		return
	}
	// we construct the bridge addr
	ev = ebMapVal{fmt.Sprintf("%v:%v", strings.Split(bridgeHost, ":")[0], port), randCookie}
	ebCache.SetDefault(mapKeyStr, ev)
	return
}
