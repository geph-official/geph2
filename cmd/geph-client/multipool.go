package main

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/geph-official/geph2/libs/backedtcp"
	log "github.com/sirupsen/logrus"
	"github.com/xtaci/smux"
)

const mpSize = 1

type mpMember struct {
	session *smux.Session
	btcp    *backedtcp.Socket
	score   float64
}

type multipool struct {
	members  chan mpMember
	metasess [32]byte

	worstPing     time.Duration
	worstPingTime time.Time
	worstPingLock sync.Mutex
}

func (mp *multipool) getWorstPing() time.Duration {
	mp.worstPingLock.Lock()
	defer mp.worstPingLock.Unlock()
	return mp.worstPing
}

func (mp *multipool) setPing(d time.Duration) {
	mp.worstPingLock.Lock()
	defer mp.worstPingLock.Unlock()
	now := time.Now()
	if d > mp.worstPing || now.Sub(mp.worstPingTime).Seconds() > 10 {
		mp.worstPingTime = now
		mp.worstPing = d
	}
}

func newMultipool() *multipool {
	tr := &multipool{}
	tr.members = make(chan mpMember, mpSize)
	rand.Read(tr.metasess[:])
	for i := 0; i < mpSize; i++ {
		var sessid [32]byte
		rand.Read(sessid[:])
		first := true
		getConn := func() (net.Conn, error) {
			log.Println("getConn called")
		retry:
			conn, err := getCleanConn()
			if err != nil {
				log.Println("failed getCleanConn():", err)
				time.Sleep(time.Second)
				goto retry
			}
			var greeting struct {
				MetaSess [32]byte
				SessID   [32]byte
			}
			greeting.MetaSess = tr.metasess
			greeting.SessID = sessid
			binary.Write(conn, binary.BigEndian, greeting)
			var response byte
			err = binary.Read(conn, binary.BigEndian, &response)
			if err != nil {
				log.Println("failed response read:", err)
				time.Sleep(time.Second)
				goto retry
			}
			log.Println("RESPONSE is", response)
			if !first && response != 1 {
				return nil, errors.New("bad")
			}
			first = false
			return conn, err
		}
		btcp := backedtcp.NewSocket(getConn)
		log.Println("btcp obtained")
		sm, err := smux.Client(btcp, &smux.Config{
			Version:           2,
			KeepAliveInterval: time.Minute * 20,
			KeepAliveTimeout:  time.Minute * 40,
			MaxFrameSize:      32768,
			MaxReceiveBuffer:  100 * 1024 * 1024,
			MaxStreamBuffer:   100 * 1024 * 1024,
		})
		if err != nil {
			panic(err)
		}
		tr.members <- mpMember{
			session: sm,
			btcp:    btcp,
			score:   0,
		}
	}
	tr.worstPing = time.Second * 5
	return tr
}

func (mp *multipool) DialCmd(cmds ...string) (conn net.Conn, ok bool) {
	mem := <-mp.members
	worst := mp.getWorstPing()
	// repeatedly reset the underlying connection
	success := make(chan bool)
	go func() {
		for {
			log.Println("worst ping", worst)
			select {
			case <-time.After(worst + time.Second*2):
				log.Println("forcing replacement!")
				go mem.btcp.Reset()
			case <-success:
				return
			}
		}
	}()
	defer close(success)
	start := time.Now()
	stream, err := mem.session.OpenStream()
	if err != nil {
		log.Println("error while opening stream, throwing away:", err.Error())
		panic("DO SOMETHING HERE")
		return nil, false
	}
	defer func() {
		mp.members <- mem
	}()
	rlp.Encode(stream, cmds)
	var connected bool
	stream.SetDeadline(time.Now().Add(time.Millisecond*time.Duration(worst) + time.Second*10))
	err = rlp.Decode(stream, &connected)
	stream.SetDeadline(time.Time{})
	mp.setPing(time.Since(start))
	return stream, err == nil
}

// get a clean, authenticated channel all the way to the exit
func getCleanConn() (conn net.Conn, err error) {
	log.Println("getCleanConn")
	ubsig, ubmsg, err := getGreeting()
	if err != nil {
		return
	}
	log.Println("getCleanConn: getGreeting returned")
	var rawConn net.Conn
	if direct {
		rawConn, err = net.DialTimeout("tcp4", exitName+":2389", time.Second*2)
	} else {
		bridges, e := bindClient.GetBridges(ubmsg, ubsig)
		if e != nil {
			err = e
			log.Warnln("getting bridges failed, retrying", err)
			return
		}
		rawConn, err = getSingleTCP(bridges)
	}
	if err != nil {
		return
	}
	log.Println("exit dialed")
	rawConn.SetDeadline(time.Now().Add(time.Second * 10))
	cryptConn, err := negotiateTinySS(&[2][]byte{ubsig, ubmsg}, rawConn, exitPK(), 'R')
	if err != nil {
		log.Println("error while negotiating cryptConn", err)
		return
	}
	rawConn.SetDeadline(time.Time{})
	conn = cryptConn
	log.Println("getCleanConn return")
	return
}

func exitPK() []byte {
	realExitKey, err := hex.DecodeString(exitKey)
	if err != nil {
		panic(err)
	}
	return realExitKey
}
