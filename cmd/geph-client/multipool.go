package main

import (
	"crypto/rand"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/geph-official/geph2/libs/backedtcp"
	"github.com/geph-official/geph2/libs/cshirt2"
	"github.com/geph-official/geph2/libs/tinysocks"
	log "github.com/sirupsen/logrus"
	"github.com/xtaci/smux"
)

type mpMember struct {
	session *smux.Session
	btcp    *backedtcp.Socket
	score   float64
}

type multipool struct {
	pool     chan *smux.Session
	metasess [32]byte
}

func newMultipool() *multipool {
	tr := &multipool{}
	tr.pool = make(chan *smux.Session, 256)
	rand.Read(tr.metasess[:])
	go func() {
		for i := 0; i < 8; i++ {
			tr.fillOne()
		}
	}()
	return tr
}

func (mp *multipool) fillOne() {
	getConn := func() net.Conn {
	retry:
		conn, err := getCleanConn()
		if err != nil {
			log.Println("failed getCleanConn():", err)
			time.Sleep(time.Second)
			goto retry
		}
		return conn
	}
	btcp := getConn()
	btcp.Write(mp.metasess[:])
	sm, err := smux.Client(btcp, &smux.Config{
		Version:           2,
		KeepAliveInterval: time.Minute * 10,
		KeepAliveTimeout:  time.Minute * 40,
		MaxFrameSize:      32768,
		MaxReceiveBuffer:  1000 * 1024,
		MaxStreamBuffer:   1000 * 1024,
	})
	if err != nil {
		panic(err)
	}
	mp.pool <- sm
}

func (mp *multipool) DialCmd(cmds ...string) (conn net.Conn, remAddr string, ok bool) {
	const RESET = 1500
	timeout := time.Millisecond * RESET
	for {
		sm := <-mp.pool
		stream, err := sm.OpenStream()
		if err != nil {
			sm.Close()
			log.Println("error while opening stream, throwing away:", err.Error())
			go mp.fillOne()
			continue
		}
		mp.pool <- sm
		rlp.Encode(stream, cmds)
		var connected bool
		// we try to connect to the other end within 500 milliseconds
		// if we time out, we move on.
		// but if we encounter any other error, we close the connection and spawn a new one.
		stream.SetDeadline(time.Now().Add(timeout))
		err = rlp.Decode(stream, &connected)
		if err != nil {
			if strings.Contains(err.Error(), "timeout") && timeout < time.Second*5 {
				log.Debugln("timeout after", timeout, "so let's try again")
				timeout = timeout * 2
				continue
			}
			log.Println("error while waiting for stream, throwing away:", err.Error())
			sm.Close()
			timeout = time.Millisecond * RESET
			continue
		}
		stream.SetDeadline(time.Time{})
		return stream, sm.RemoteAddr().String(), true
	}
}

var cleanHTTPClient = &http.Client{
	Transport: &http.Transport{
		Proxy:           nil,
		IdleConnTimeout: time.Second * 120,
	},
	Timeout: time.Second * 120,
}

// get a clean, authenticated channel all the way to the exit
func getCleanConn() (conn net.Conn, err error) {
	var rawConn net.Conn
	if singleHop != "" {
		splitted := strings.Split(singleHop, "@")
		if len(splitted) != 2 {
			panic("-singleHop must be pk@host")
		}
		var tcpConn net.Conn
		var e error
		if upstreamProxy != "" {
			tcpConn, e = net.DialTimeout("tcp", upstreamProxy, time.Second*5)
			if e != nil {
				log.Warnln("failed to connect to SOCKS5 font proxy server:", e)
				err = e
				return
			}
			e, _ = tinysocks.Client(tcpConn, tinysocks.ParseAddr(splitted[1]), tinysocks.CmdConnect)
			if e != nil {
				tcpConn.Close()
				log.Warnln("failed handshake with second SOCKS5 server:", e)
				err = e
				return
			}
		} else {
			tcpConn, e = net.DialTimeout("tcp", splitted[1], time.Second*5)
			if e != nil {
				log.Warn("cannot connect to singleHop server:", e)
				err = e
				return
			}
		}
		tcpConn.SetDeadline(time.Now().Add(time.Second * 10))
		pk, e := hex.DecodeString(splitted[0])
		if e != nil {
			panic(e)
		}
		obfsConn, e := cshirt2.Client(pk, tcpConn)
		if e != nil {
			log.Warn("cannot negotiate cshirt2 with singleHop server:", e)
			err = e
			return
		}
		cryptConn, e := negotiateTinySS(nil, obfsConn, pk, 'N')
		if e != nil {
			log.Warn("cannot negotiate tinyss with singleHop server:", e)
			err = e
			return
		}
		conn = cryptConn
		return
	}
	ubsig, ubmsg, err := getGreeting()
	if err != nil {
		return
	}

	if direct {
		if upstreamProxy != "" {
			rawConn, err = net.DialTimeout("tcp", upstreamProxy, time.Second*5)
			if err != nil {
				log.Warnln("failed to connect to singlehop server:", err)
				return
			}
			err, _ = tinysocks.Client(rawConn, tinysocks.ParseAddr(exitName+":2389"), tinysocks.CmdConnect)
			if err != nil {
				rawConn.Close()
				log.Warnln("failed handshake with second SOCKS5 server:", err)
				return
			}
		} else {
			rawConn, err = net.DialTimeout("tcp", exitName+":2389", time.Second*5)
			if err != nil {
				log.Warnln("failed to connect to exit server: %v", err)
				return
			}
		}
		if err == nil {
			rawConn.(*net.TCPConn).SetKeepAlive(false)
		}
	} else {
		getWarpfrontCon := func() (warpConn net.Conn, err error) {
			var wfstuff map[string]string
			wfstuff, err = getBindClient().GetWarpfronts()
			if err != nil {
				log.Warnln("can't get warp front:", err)
				return
			}
			warpConn, err = getWarpfront(wfstuff)
			return
		}
		if forceWarpfront {
			rawConn, err = getWarpfrontCon()
			if err != nil {
				return
			}
		} else {
			bridges, e := getBridges(ubmsg, ubsig)
			if e != nil {
				err = e
				log.Warnln("getting bridges failed, retrying", err)
				return
			}
			rawConn, err = getSingleTCP(bridges)
			if err != nil {
				log.Warnf("can't connect to bridges (%v); time to W A R P F R O N T", err)
				rawConn, err = getWarpfrontCon()
				if err != nil {
					return
				}
			}
		}
	}
	rawConn.SetDeadline(time.Now().Add(time.Second * 10))
	cryptConn, err := negotiateTinySS(&[2][]byte{ubsig, ubmsg}, rawConn, exitPK(), 'N')
	if err != nil {
		log.Println("error while negotiating cryptConn", err)
		return
	}
	rawConn.SetDeadline(time.Time{})
	conn = cryptConn
	log.Debugln("new conn to", conn.RemoteAddr())
	return
}

func exitPK() []byte {
	realExitKey, err := hex.DecodeString(exitKey)
	if err != nil {
		panic(err)
	}
	return realExitKey
}
