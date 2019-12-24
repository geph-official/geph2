package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/geph-official/geph2/libs/kcp-go"
	"github.com/geph-official/geph2/libs/niaucchi4"
	"github.com/geph-official/geph2/libs/tinyss"
	"github.com/xtaci/smux"
)

func getDirect(greeting [2][]byte, host string, pk []byte) (ss *smux.Session, err error) {
	var conn net.Conn
	if useTCP {
		conn, err = net.Dial("tcp", host+":2389")
		if err != nil {
			return
		}
		ss, err = negotiateSmux(greeting, conn, pk)
		return
	}
	conn, err = niaucchi4.DialKCP(host+":2389", make([]byte, 32))
	if err != nil {
		err = fmt.Errorf("plain TCP failed: %w", err)
		return
	}
	ss, err = negotiateSmux(greeting, conn, pk)
	return

}

func negotiateSmux(greeting [2][]byte, rawConn net.Conn, pk []byte) (ss *smux.Session, err error) {
	rawConn.SetDeadline(time.Now().Add(time.Second * 20))
	cryptConn, err := tinyss.Handshake(rawConn, 0)
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
		log.Println("authentication failed", reply)
		os.Exit(11)
	}
	smuxConf := &smux.Config{
		KeepAliveInterval: time.Minute * 20,
		KeepAliveTimeout:  time.Minute * 22,
		MaxFrameSize:      4096,
		MaxReceiveBuffer:  100 * 1024 * 1024,
	}
	ss, err = smux.Client(cryptConn, smuxConf)
	if err != nil {
		rawConn.Close()
		err = fmt.Errorf("smux error: %w", err)
		return
	}
	rawConn.SetDeadline(time.Now().Add(time.Hour * 12))
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
			log.Errorln("error authenticating:", err)
			if errors.Is(err, io.EOF) {
				os.Exit(11)
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
				log.Warnln("direct conn retrying", err)
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
			log.Warnln("getting bridges failed, retrying", err)
			time.Sleep(time.Second)
			goto retry
		}
		log.Debugln("racing between", len(bridges), "bridges...")
		bridgeRace := make(chan bool)
		bridgeDeadWait := new(sync.WaitGroup)
		bridgeDeadWait.Add(len(bridges))
		usocket := niaucchi4.Wrap(func() net.PacketConn {
			us, err := net.ListenPacket("udp", ":")
			if err != nil {
				panic(err)
			}
			return us
		})
		cookie := make([]byte, 32)
		rand.Read(cookie)
		osocket := niaucchi4.ObfsListen(cookie, usocket)
		e2esid := niaucchi4.NewSessAddr()
		e2e := niaucchi4.NewE2EConn(osocket)
		go func() {
			bridgeDeadWait.Wait()
			close(bridgeRace)
		}()
		for _, bi := range bridges {
			bi := bi
			go func() {
				defer bridgeDeadWait.Done()
				kcpConn, err := niaucchi4.DialKCP(bi.Host, bi.Cookie)
				if err != nil {
					log.Debug("dialing to", bi.Host, "failed!")
					return
				}
				defer kcpConn.Close()
				kcpConn.SetDeadline(time.Now().Add(time.Second * 30))
				rlp.Encode(kcpConn, "conn/e2e")
				rlp.Encode(kcpConn, exitName)
				rlp.Encode(kcpConn, cookie)
				var port uint
				e := rlp.Decode(kcpConn, &port)
				if e != nil {
					log.Warnln("conn/e2e to", bi.Host, "failed:", e)
					return
				}
				complete := fmt.Sprintf("%v:%v", strings.Split(bi.Host, ":")[0], port)
				compudp, err := net.ResolveUDPAddr("udp", complete)
				if err != nil {
					log.Println("cannot resolve udp for", complete, err)
					return
				}
				log.Debugln("adding", complete, "to our e2e")
				e2e.SetSessPath(e2esid, compudp)
				select {
				case bridgeRace <- true:
				default:
				}
			}()
		}
		// get the bridge
		_, ok := <-bridgeRace
		if !ok {
			log.Println("everything failed, retrying")
			time.Sleep(time.Second)
			goto retry
		}
		kcpConn, err := kcp.NewConn2(e2esid, nil, 0, 0, e2e)
		if err != nil {
			panic(err)
		}
		kcpConn.SetWindowSize(1000, 10000)
		kcpConn.SetNoDelay(0, 50, 3, 0)
		kcpConn.SetStreamMode(true)
		kcpConn.SetMtu(1300)
		sm, err := negotiateSmux([2][]byte{ubmsg, ubsig}, kcpConn, realExitKey)
		if err != nil {
			log.Println("Failed negotiating smux:", err)
			kcpConn.Close()
			goto retry
		}
		kcpConn.SetDeadline(time.Time{})
		useStats(func(sc *stats) {
			sc.Connected = true
		})
		return sm
	}}
}
