package kcp

import (
	"sync/atomic"
	"time"

	"github.com/pkg/errors"
	"golang.org/x/net/ipv4"
)

func (s *UDPSession) paceOnce(n int) {
	paceInterval := float64(s.kcp.mss) / s.kcp.DRE.maxAckRate
	if paceInterval > 0.001 {
		paceInterval = 0.001
	}
	// wait till NST
	now := time.Now()
	if !now.After(s.pacer.nextSendTime) {
		time.Sleep(s.pacer.nextSendTime.Sub(now))
	}
	s.pacer.nextSendTime = now.Add(time.Duration(float64(n)*paceInterval*1e6/s.kcp.LOL.gain) * time.Microsecond)
}

func (s *UDPSession) defaultTx(txqueue []ipv4.Message) {
	nbytes := 0
	npkts := 0
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
	atomic.AddUint64(&DefaultSnmp.OutPkts, uint64(npkts))
	atomic.AddUint64(&DefaultSnmp.OutBytes, uint64(nbytes))
}
