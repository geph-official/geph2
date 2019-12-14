package kcp

import (
	"log"
	"time"
)

func (kcp *KCP) lwkPeriodic() {
	now := time.Now()
	beta := 2.0
	if now.Sub(kcp.LWK.lastUpdate) > time.Millisecond*500 {
		kcp.LWK.lastUpdate = now
		// create loss estimate
		newDeltaTrans := float64(kcp.trans) - kcp.LWK.oldTrans
		newDeltaRetrans := float64(kcp.retrans) - kcp.LWK.oldRetrans
		if newDeltaTrans == 0 {
			return
		}
		newLoss := newDeltaRetrans / newDeltaTrans
		if newLoss <= kcp.LWK.oldLoss && kcp.cwnd < 10000 {
			kcp.cwnd *= beta
			kcp.cwnd++
		} else {
			kcp.cwnd /= beta
		}
		if kcp.cwnd < kcp.bdp()/float64(kcp.mss) {
			kcp.cwnd = kcp.bdp() / float64(kcp.mss)
		}
		log.Printf("LWK newLoss = %.2f%%, oldLoss = %.2f%%, cwnd = %v", newLoss*100, kcp.LWK.oldLoss*100, int(kcp.cwnd))
		kcp.LWK.oldTrans, kcp.LWK.oldRetrans, kcp.LWK.oldLoss = float64(kcp.trans), float64(kcp.retrans), newLoss
	}
}
