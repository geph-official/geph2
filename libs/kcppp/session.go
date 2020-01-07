package kcppp

import (
	"log"
	"os"
	"sync"
)

var doLogging = false

const mss = 1300

func init() {
	doLogging = os.Getenv("KCPLOG") != ""
}

// session is a KCP++ session.
type session struct {
	convID     uint32
	inFlight   []annSegment
	nextFreeSN uint32
	remAckSN   uint32
	locAckSN   uint32

	rtoTimer *resettableTimer

	isDead bool

	lock sync.Mutex
	cvar *sync.Cond

	toSend       []inlineBytes
	toRecv       []inlineBytes
	sendCallback func(seg segment)
}

func newSession(convID uint32, cback func(seg segment)) *session {
	sess := &session{
		convID:   convID,
		rtoTimer: newTimer(),

		sendCallback: cback,
	}
	sess.cvar = sync.NewCond(&sess.lock)
	go sess.writeLoop()
	return sess
}

// annSegment is an "annotated" segment with state info.
type annSegment struct {
	seg          segment
	acked        bool
	retransTimes int
}

// writeLoop is the main writing loop of the session.
func (sess *session) writeLoop() {
	sess.lock.Lock()
	defer sess.lock.Unlock()
	defer func() {
		sess.isDead = true
		sess.cvar.Broadcast()
	}()
	// maximum send window size
	maxInflight := 1000
	for !sess.isDead {
		// first, wait for space in window
		for len(sess.inFlight) >= maxInflight {
			sess.cvar.Wait()
			if sess.isDead {
				return
			}
		}
		// send as much stuff as possible
		for len(sess.inFlight) < maxInflight && len(sess.toSend) > 0 {
			var seg segment
			seg.Body = sess.toSend[0]
			sess.toSend = sess.toSend[1:]
			seg.Header = segHeader{
				ConvID:    sess.convID,
				Cmd:       cmdPUSH,
				Window:    1000,
				Timestamp: currentMS(),
				Seqno:     sess.nextFreeSN,
				Ackno:     sess.locAckSN,
			}
			sess.nextFreeSN++
			sess.inFlight = append(sess.inFlight, annSegment{
				seg: seg,
			})
			sess.sendCallback(seg)
			sess.setTimer()
			if doLogging {
				log.Println(sess.convID, "sending seqno", seg.Header.Seqno)
			}
		}
	}
}

// segInput inputs a packet into the state machine
func (sess *session) segInput(seg segment) {
	// check convo id
	if seg.Header.ConvID != sess.convID {
		if doLogging {
			log.Println(sess.convID, "rejecting bad ConvID")
		}
		return
	}
	sess.lock.Lock()
	defer sess.lock.Unlock()
	if seg.Header.Cmd == cmdRST {

	}
	switch seg.Header.Cmd {
	case cmdRST:
		sess.isDead = true
		return
	case cmdACK:
	}
	// update UNA
	if seg.Header.Ackno > sess.remAckSN {
		sess.remAckSN = seg.Header.Ackno
	}
	// mark as acked
	for _, seg := range sess.inFlight {
		if diff32(sess.remAckSN, seg.seg.Header.Seqno) > 0 {
			seg.acked = true
		}
	}
	// trim acked
	for len(sess.inFlight) > 0 && sess.inFlight[0].acked {
		sess.inFlight = sess.inFlight[1:]
	}
}
