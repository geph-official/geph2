package kcppp

import (
	"log"
	"time"
)

func (sess *session) setTimer() {
	sess.rtoTimer.actions <- rtAction{
		dline: time.Now().Add(time.Second * 2),
		action: func() {
			sess.lock.Lock()
			defer sess.lock.Unlock()
			if doLogging {
				log.Println(sess.convID, "timer fired")
			}
			// send the last unacknowledged one
			for _, seg := range sess.inFlight {
				if !seg.acked {
					if doLogging {
						log.Println(sess.convID,
							"RTO sending last unack",
							seg.seg.Header.Seqno)
					}
					sess.sendCallback(seg.seg)
					seg.retransTimes++
					go sess.setTimer()
					break
				}
			}
		},
	}
}

func diff32(later, earlier uint32) int32 {
	return (int32)(later - earlier)
}
