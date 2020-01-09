package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/geph-official/geph2/libs/cshirt2"
	"github.com/geph-official/geph2/libs/tinyss"
	"github.com/xtaci/smux"
)

func negotiateSmux(greeting *[2][]byte, rawConn net.Conn, pk []byte) (ss *smux.Session, err error) {
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
		if loginCheck {
			os.Exit(0)
		}
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

func dialBridge(host string, cookie []byte) (net.Conn, error) {
	// return niaucchi4.DialKCP(host, cookie)
	conn, err := net.Dial("tcp", host)
	if err != nil {
		return nil, err
	}
	return cshirt2.Client(cookie, conn)
}

func newSmuxWrapper() *muxWrap {
	return &muxWrap{getSession: func() *smux.Session {
		useStats(func(sc *stats) {
			sc.Connected = false
			sc.bridgeThunk = nil
		})
		defer useStats(func(sc *stats) {
			sc.Connected = true
		})
	retry:
		if singleHop == "" {
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
			log.Infoln("Obtained", len(bridges), "bridges")
			for _, b := range bridges {
				log.Infof(".... %v %x", b.Host, b.Cookie)
			}
			var conn net.Conn
			if useTCP {
				conn, err = getSinglepath(bridges)
				if err != nil {
					log.Println("Singlepath failed!")
					goto retry
				}
			} else {
				conn, err = getMultipath(bridges)
				if err != nil {
					log.Println("Multipath failed!")
					goto retry
				}
			}
			sm, err := negotiateSmux(&[2][]byte{ubmsg, ubsig}, conn, realExitKey)
			if err != nil {
				log.Println("Failed negotiating smux:", err)
				conn.Close()
				goto retry
			}
			conn.SetDeadline(time.Now().Add(time.Hour * 6))
			return sm
		} else {
			splitted := strings.Split(singleHop, "@")
			lel, err := hex.DecodeString(splitted[0])
			if err != nil {
				panic(err)
			}
			lol, err := getSingleHop(splitted[1], lel)
			if err != nil {
				goto retry
			}
			return lol
		}
	}}
}
