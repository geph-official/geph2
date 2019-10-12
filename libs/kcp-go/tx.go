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
	ctr := int(atomic.LoadUint64(&DefaultSnmp.OutPkts))
	for k := range txqueue {
		if n, err := s.conn.WriteTo(txqueue[k].Buffers[0], txqueue[k].Addr); err == nil {
			nbytes += n
			npkts++
			if (npkts+ctr)%sendQuantum == 0 && CongestionControl == "LOL" {
				paceInterval := float64(s.kcp.mss) / s.kcp.DRE.maxAckRate
				if paceInterval > 0.001 {
					paceInterval = 0.001
				}
				// wait till NST
				now := time.Now()
				if !now.After(s.pacer.nextSendTime) {
					time.Sleep(s.pacer.nextSendTime.Sub(now))
				}
				s.pacer.nextSendTime = now.Add(time.Duration(paceInterval*1e6*sendQuantum/s.kcp.LOL.gain) * time.Microsecond)
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
