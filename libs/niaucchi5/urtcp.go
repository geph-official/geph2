package niaucchi5

import (
	"bufio"
	"encoding/binary"
	"io"
	"log"
	"math"
	"math/rand"
	"net"
	"sync"
	"time"

	"github.com/bep/debounce"
	pool "github.com/libp2p/go-buffer-pool"
	"gopkg.in/tomb.v1"
)

// alias for separating heap-managed and pool-managed slices
type poolSlice []byte

const maxSegmentSize = 16384
const flagAck = 30001
const flagPing = 40001
const flagPong = 40002

var delayer = debounce.New(50 * time.Millisecond)

// URTCP implements "unreliable TCP". This is an unreliable PacketWire implementation over reliable net.Conn's like TCP, yet avoids excessive bufferbloat, "TCP over TCP" problems, etc.
type URTCP struct {
	wire     net.Conn
	wireRead io.Reader

	// synchronization
	cvar     *sync.Cond
	death    tomb.Tomb
	deatherr error

	// sending variables
	sv struct {
		sendBuffer    chan poolSlice // includes 2-byte length header
		inflight      int
		inflightLimit int
		delivered     uint64
		lastAckNano   uint64

		pingSendNano   uint64
		ping           uint64
		pingUpdateTime time.Time

		bw           float64
		bwUpdateTime time.Time
	}

	// receiving variables
	rv struct {
		recvBuffer  []poolSlice
		unacked     int
		lastAckTime time.Time
	}
}

// NewURTCP creates a new URTCP instance.
func NewURTCP(wire net.Conn) *URTCP {
	tr := &URTCP{
		wire:     wire,
		wireRead: bufio.NewReaderSize(wire, 4096),
	}
	tr.cvar = sync.NewCond(new(sync.Mutex))
	tr.sv.sendBuffer = make(chan poolSlice, 1048576)
	tr.sv.inflightLimit = 100 * 1024
	go func() {
		<-tr.death.Dying()
		tr.cvar.L.Lock()
		tr.deatherr = tr.death.Err()
		tr.cvar.Broadcast()
		tr.cvar.L.Unlock()
	}()
	go tr.sendLoop()
	go tr.recvLoop()
	return tr
}

// SendSegment sends a single segment.
func (tr *URTCP) SendSegment(seg []byte, block bool) (err error) {
	tr.cvar.L.Lock()
	defer tr.cvar.L.Unlock()
	defer tr.cvar.Broadcast()
	if block {
		for tr.sv.inflight > tr.sv.inflightLimit && tr.deatherr == nil {
			tr.cvar.Wait()
		}
	} else {
		// random early detection
		minThresh := 0.8
		dropProb := math.Max(0, float64(tr.sv.inflight)/float64(tr.sv.inflightLimit)-minThresh) / (1 - minThresh)
		if rand.Float64() < dropProb {
			return
		}
	}

	if tr.deatherr != nil {
		err = tr.deatherr
		return
	}
	// we have free space now, enqueue for sending
	segclone := poolSlice(pool.Get(len(seg) + 2))
	binary.LittleEndian.PutUint16(segclone[:2], uint16(len(seg)))
	copy(segclone[2:], seg)
	tr.queueForSend(segclone)
	// send req for ack if needed
	tr.sv.inflight += len(seg)
	if tr.sv.pingSendNano == 0 {
		tr.sv.pingSendNano = uint64(time.Now().UnixNano())
		ping := poolSlice(pool.Get(2))
		binary.LittleEndian.PutUint16(ping, flagPing)
		tr.queueForSend(ping)
	}
	return
}

// RecvSegment receives a single segment.
func (tr *URTCP) RecvSegment(seg []byte) (n int, err error) {
	tr.cvar.L.Lock()
	defer tr.cvar.L.Unlock()
	for len(tr.rv.recvBuffer) == 0 && tr.deatherr == nil {
		tr.cvar.Wait()
	}
	if tr.deatherr != nil {
		err = tr.deatherr
		return
	}
	fs := tr.rv.recvBuffer[0]
	tr.rv.recvBuffer = tr.rv.recvBuffer[1:]
	n = copy(seg, fs)
	pool.Put(fs)
	return
}

func (tr *URTCP) queueForSend(b poolSlice) {
	select {
	case tr.sv.sendBuffer <- b:
	default:
		go func() {
			select {
			case tr.sv.sendBuffer <- b:
			case <-tr.death.Dying():
			}
		}()
	}
}

func (tr *URTCP) sendLoop() {
	defer tr.wire.Close()
	for {
		select {
		case toSend := <-tr.sv.sendBuffer:
			if len(toSend) > maxSegmentSize+2 {
				panic("shouldn't happen")
			}
			_, err := tr.wire.Write(toSend)
			if err != nil {
				tr.death.Kill(err)
				tr.cvar.Broadcast()
				return
			}
			pool.Put(toSend)
		case <-tr.death.Dying():
			return
		}
	}
}

func (tr *URTCP) forceAck() {
	if tr.rv.unacked > 0 {
		ack := poolSlice(pool.Get(2 + 8 + 8))
		binary.LittleEndian.PutUint16(ack[:2], flagAck)
		binary.LittleEndian.PutUint64(ack[2:][:8], uint64(time.Now().UnixNano()))
		binary.LittleEndian.PutUint64(ack[2:][8:], uint64(tr.rv.unacked))
		tr.rv.unacked = 0
		tr.queueForSend(ack)
		tr.rv.lastAckTime = time.Now()
	}
}

func (tr *URTCP) delayedAck() {
	delayer(func() {
		tr.cvar.L.Lock()
		defer tr.cvar.L.Unlock()
		tr.forceAck()
	})
}

func (tr *URTCP) recvLoop() {
	defer tr.wire.Close()
	defer tr.cvar.Broadcast()
	defer tr.death.Kill(io.ErrClosedPipe)
	lenbuf := make([]byte, 2)
	for {
		_, err := io.ReadFull(tr.wireRead, lenbuf)
		if err != nil {
			return
		}
		lenint := binary.LittleEndian.Uint16(lenbuf)
		switch lenint {
		case flagPing:
			pingPkt := pool.Get(8)
			binary.LittleEndian.PutUint16(pingPkt, flagPong)
			tr.queueForSend(pingPkt)
		case flagPong:
			tr.cvar.L.Lock()
			now := time.Now()
			pingSample := uint64(time.Now().UnixNano()) - tr.sv.pingSendNano
			if pingSample < tr.sv.ping || now.Sub(tr.sv.pingUpdateTime) > time.Second*30 {
				tr.sv.ping = pingSample
				tr.sv.pingUpdateTime = now
			}
			log.Println("********* ping sample", float64(pingSample)*1e-9*1000)
			tr.sv.pingSendNano = 0
			tr.cvar.L.Unlock()
		case flagAck:
			rUnixNanoB := pool.Get(8 + 8)
			_, err = io.ReadFull(tr.wireRead, rUnixNanoB)
			if err != nil {
				return
			}
			rUnixNano := binary.LittleEndian.Uint64(rUnixNanoB[:8])
			rAckCount := binary.LittleEndian.Uint64(rUnixNanoB[8:])
			pool.Put(rUnixNanoB)

			tr.cvar.L.Lock()
			//now := time.Now()

			ping := float64(tr.sv.ping) * 1e-9

			deltaD := float64(rAckCount)
			tr.sv.delivered += uint64(deltaD)
			tr.sv.inflight -= int(rAckCount)
			deltaT := float64(rUnixNano - tr.sv.lastAckNano)
			tr.sv.lastAckNano = rUnixNano
			bwSample := 1e9 * deltaD / deltaT
			// if bwSample > tr.sv.bw || now.Sub(tr.sv.bwUpdateTime).Seconds() > ping*10 {
			// 	tr.sv.bw = bwSample
			// 	tr.sv.bwUpdateTime = now
			// }
			if bwSample > tr.sv.bw {
				tr.sv.bw = bwSample*0.5 + tr.sv.bw*0.5
			} else {
				tr.sv.bw = bwSample*0.1 + tr.sv.bw*0.9
			}

			Bps := tr.sv.bw
			log.Println("bw sample", int(Bps/1000), "KB/s")
			bdp := Bps * ping
			tgtIFL := int(bdp*3) + 100*1024
			tr.sv.inflightLimit = tgtIFL
			// if rand.Int()%10 == 0 {
			// 	tr.sv.inflightLimit = int(bdp)
			// 	log.Println("SEVERE LIMITING")
			// }

			tr.cvar.Broadcast()
			tr.cvar.L.Unlock()

		default:
			body := poolSlice(pool.Get(int(lenint)))
			_, err = io.ReadFull(tr.wireRead, body)
			if err != nil {
				return
			}
			// notify the world
			tr.cvar.L.Lock()
			tr.rv.recvBuffer = append(tr.rv.recvBuffer, body)
			tr.rv.unacked += len(body)
			if time.Since(tr.rv.lastAckTime) > time.Millisecond*20 {
				tr.forceAck()
			} else {
				tr.delayedAck()
			}
			tr.cvar.Broadcast()
			tr.cvar.L.Unlock()
		}
	}
}
