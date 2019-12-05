package kcp

import (
	"sync/atomic"
	"time"

	"github.com/pkg/errors"
	"golang.org/x/net/ipv4"
)

func (s *UDPSession) paceOnce(bytes int) {
	paceInterval := float64(bytes) / s.kcp.DRE.maxAckRate
	if paceInterval > 0.001 {
		paceInterval = 0.001
	}
	// wait till NST
	now := time.Now()
	if !now.After(s.pacer.nextSendTime) {
		time.Sleep(s.pacer.nextSendTime.Sub(now))
	}
	ival := paceInterval * 1e6 / s.kcp.LOL.gain
	s.pacer.nextSendTime = now.Add(time.Duration(ival) * time.Microsecond)
}

func (s *UDPSession) defaultTx(txqueue []ipv4.Message) {
	nbytes := 0
	npkts := 0
	//start := time.Now()
	//divider := len(txqueue)
	for k := range txqueue {
		if n, err := s.conn.WriteTo(txqueue[k].Buffers[0], txqueue[k].Addr); err == nil {
			nbytes += n
			npkts++
			xmitBuf.Put(txqueue[k].Buffers[0])
		} else {
			s.notifyWriteError(errors.WithStack(err))
			break
		}
	}
	//log.Println("tx in", time.Since(start)/time.Duration(divider))
	atomic.AddUint64(&DefaultSnmp.OutPkts, uint64(npkts))
	atomic.AddUint64(&DefaultSnmp.OutBytes, uint64(nbytes))
}
