package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/geph-official/geph2/libs/bdclient"
	"github.com/geph-official/geph2/libs/cshirt2"
	"github.com/geph-official/geph2/libs/tinyss"
)

func negotiateTinySS(greeting *[2][]byte, rawConn net.Conn, pk []byte, nextProto byte) (cryptConn *tinyss.Socket, err error) {
	rawConn.SetDeadline(time.Now().Add(time.Second * 20))
	cryptConn, err = tinyss.Handshake(rawConn, nextProto)
	if err != nil {
		err = fmt.Errorf("tinyss handshake failed: %w", err)
		rawConn.Close()
		return
	}
	// verify the actual msg
	var sssig []byte
	err = rlp.Decode(cryptConn, &sssig)
	if err != nil {
		err = fmt.Errorf("cannot decode sssig: %w", err)
		rawConn.Close()
		return
	}
	if !ed25519.Verify(pk, cryptConn.SharedSec(), sssig) {
		err = errors.New("man in the middle")
		rawConn.Close()
		return
	}
	if greeting != nil {
		// send the greeting
		rlp.Encode(cryptConn, greeting)
		// wait for the reply
		var reply string
		err = rlp.Decode(cryptConn, &reply)
		if err != nil {
			err = fmt.Errorf("cannot decode reply: %w", err)
			rawConn.Close()
			return
		}
		if reply != "OK" {
			err = errors.New("authentication failed")
			rawConn.Close()
			log.Println("authentication failed", reply)
			os.Exit(11)
		}
	}
	return
}

func dialBridge(host string, cookie []byte) (net.Conn, error) {
	// return niaucchi4.DialKCP(host, cookie)
	conn, err := net.DialTimeout("tcp", host, time.Second*3)
	if err != nil {
		return nil, err
	}
	conn.(*net.TCPConn).SetKeepAlive(false)
	return cshirt2.Client(cookie, conn)
}

var greetingCache struct {
	ubmsg   []byte
	ubsig   []byte
	expires time.Time
	lock    sync.Mutex
}

func getGreeting() (ubmsg, ubsig []byte, err error) {
	greetingCache.lock.Lock()
	defer greetingCache.lock.Unlock()
	if time.Now().Before(greetingCache.expires) {
		ubmsg, ubsig = greetingCache.ubmsg, greetingCache.ubsig
		return
	}
	// obtain a ticket
	ubmsg, ubsig, details, err := bindClient.GetTicket(username, password)
	if err != nil {
		log.Errorln("error authenticating:", err)
		if errors.Is(err, bdclient.ErrBadAuth) && loginCheck {
			os.Exit(11)
		}
		return
	}
	if loginCheck {
		os.Exit(0)
	}
	useStats(func(sc *stats) {
		sc.Username = username
		sc.Expiry = details.PaidExpiry
		sc.Tier = details.Tier
		sc.PayTxes = details.Transactions
	})
	greetingCache.ubmsg = ubmsg
	greetingCache.ubsig = ubsig
	greetingCache.expires = time.Now().Add(time.Second * 30)
	return
}

var bridgesCache struct {
	bridges []bdclient.BridgeInfo
	expires time.Time
	lock    sync.Mutex
}

func getBridges(ubmsg, ubsig []byte) ([]bdclient.BridgeInfo, error) {
	bridgesCache.lock.Lock()
	defer bridgesCache.lock.Unlock()
	if time.Now().Before(bridgesCache.expires) {
		return bridgesCache.bridges, nil
	}
	bridges, e := bindClient.GetBridges(ubmsg, ubsig)
	if e != nil {
		return nil, e
	}
	log.Infoln("Obtained", len(bridges), "bridges")
	for _, b := range bridges {
		log.Infof(".... %v %x", b.Host, b.Cookie)
	}
	if additionalBridges != "" {
		relays := strings.Split(additionalBridges, ";")
		for _, str := range relays {
			splitted := strings.Split(str, "@")
			if len(splitted) != 2 {
				panic("-additionalBridges must be cookie@host;XX;XX")
			}
			cookie, err := hex.DecodeString(splitted[0])
			if err != nil {
				panic(err)
			}
			bridges = append(bridges, bdclient.BridgeInfo{Cookie: cookie, Host: splitted[1]})
		}
	}
	bridgesCache.bridges, bridgesCache.expires = bridges, time.Now().Add(time.Minute)
	return bridges, nil
}
