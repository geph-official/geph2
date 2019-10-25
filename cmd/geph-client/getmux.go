package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/geph-official/geph2/libs/niaucchi4"
	"github.com/geph-official/geph2/libs/tinyss"
	"github.com/xtaci/smux"
)

func getBridged(greeting [2][]byte, bridgeHost string, bridgeCookie []byte, exitName string, exitPK []byte) (ss *smux.Session, err error) {
	udpsock, err := net.ListenPacket("udp", "")
	if err != nil {
		panic(err)
	}
	obfssock := niaucchi4.ObfsListen(bridgeCookie, udpsock)
	kcpConn, err := niaucchi4.Dial(bridgeHost, bridgeCookie)
	if err != nil {
		obfssock.Close()
		return
	}
	rlp.Encode(kcpConn, "conn")
	rlp.Encode(kcpConn, exitName)
	ss, err = negotiateSmux(greeting, kcpConn, exitPK)
	return
}

func getDirect(greeting [2][]byte, host string, pk []byte) (ss *smux.Session, err error) {
	kcpConn, err := niaucchi4.Dial(host+":2389", make([]byte, 32))
	if err != nil {
		err = fmt.Errorf("plain TCP failed: %w", err)
		return
	}
	ss, err = negotiateSmux(greeting, kcpConn, pk)
	return
}

func negotiateSmux(greeting [2][]byte, rawConn net.Conn, pk []byte) (ss *smux.Session, err error) {
	rawConn.SetDeadline(time.Now().Add(time.Second * 10))
	cryptConn, err := tinyss.Handshake(rawConn)
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
		os.Exit(403)
	}
	ss, err = smux.Client(cryptConn, &smux.Config{
		KeepAliveInterval: time.Minute * 20,
		KeepAliveTimeout:  time.Minute * 22,
		MaxFrameSize:      10000,
		MaxReceiveBuffer:  1024 * 1024 * 100,
	})
	if err != nil {
		rawConn.Close()
		err = fmt.Errorf("smux error: %w", err)
		return
	}
	rawConn.SetDeadline(time.Time{})
	return
}

func newSmuxWrapper() *muxWrap {
	return &muxWrap{getSession: func() *smux.Session {
		useStats(func(sc *stats) {
			sc.Connected = false
		})
	retry:
		// obtain a ticket
		ubmsg, ubsig, details, err := bindClient.GetTicket(username, password)
		if err != nil {
			log.Println("error authenticating:", err)
			if errors.Is(err, io.EOF) {
				os.Exit(403)
			}
			time.Sleep(time.Second)
			goto retry
		}
		useStats(func(sc *stats) {
			sc.Username = username
			sc.Expiry = details.PaidExpiry
			sc.Tier = details.Tier
			sc.PayTxes = details.Transactions
		})
		realExitKey, err := hex.DecodeString(exitKey)
		if err != nil {
			panic(err)
		}
		if direct {
			sm, err := getDirect([2][]byte{ubmsg, ubsig}, exitName, realExitKey)
			if err != nil {
				log.Println("direct conn retrying", err)
				time.Sleep(time.Second)
				goto retry
			}
			useStats(func(sc *stats) {
				sc.Connected = true
			})
			return sm
		}
		bridges, err := bindClient.GetBridges(ubmsg, ubsig)
		if err != nil {
			log.Println("getting bridges failed, retrying", err)
			time.Sleep(time.Second)
			goto retry
		}
		log.Println("racing between", len(bridges), "bridges...")
		bridgeRace := make(chan *smux.Session)
		bridgeDeadWait := new(sync.WaitGroup)
		bridgeDeadWait.Add(len(bridges))
		go func() {
			bridgeDeadWait.Wait()
			close(bridgeRace)
		}()
		for _, bi := range bridges {
			bi := bi
			go func() {
				defer bridgeDeadWait.Done()
				sm, err := getBridged([2][]byte{ubmsg, ubsig}, bi.Host, bi.Cookie, exitName, realExitKey)
				if err != nil {
					log.Println("dialing to", bi.Host, "failed!")
					return
				}
				select {
				case bridgeRace <- sm:
					log.Println(bi.Host, "won race!")
				default:
					log.Println(bi.Host, "lost race")
					sm.Close()
				}
			}()
		}
		// get the bridge
		sm, ok := <-bridgeRace
		if !ok {
			log.Println("everything failed, retrying")
			time.Sleep(time.Second)
			goto retry
		}
		useStats(func(sc *stats) {
			sc.Connected = true
		})
		return sm
	}}
}
