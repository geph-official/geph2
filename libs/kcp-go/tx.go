package kcp

import (
	"sync/atomic"
	"time"

	"github.com/pkg/errors"
	"golang.org/x/net/ipv4"
)

const sendQuantum = 1

func (s *UDPSession) defaultTx(txqueue []ipv4.Message) {
	nbytes := 0
	npkts := 0
	var nextSendTime time.Time
	for k := range txqueue {
		if n, err := s.conn.WriteTo(txqueue[k].Buffers[0], txqueue[k].Addr); err == nil {
			nbytes += n
			npkts++
			if DefaultSnmp.OutPkts%sendQuantum == 0 && CongestionControl == "LOL" {
				paceInterval := float64(n) / s.kcp.DRE.maxAckRate
				if paceInterval > 0.001 {
					paceInterval = 0.001
				}
				// wait till NST
				now := time.Now()
				if !now.After(nextSendTime) {
					time.Sleep(nextSendTime.Sub(now))
				}
				nextSendTime = now.Add(time.Duration(paceInterval*1e6*sendQuantum/s.kcp.LOL.gain) * time.Microsecond)
			}
			xmitBuf.Put(txqueue[k].Buffers[0])
		} else {
			s.notifyWriteError(errors.WithStack(err))
			break
		}
	}
	atomic.AddUint64(&DefaultSnmp.OutPkts, uint64(npkts))
	atomic.AddUint64(&DefaultSnmp.OutBytes, uint64(nbytes))
}
