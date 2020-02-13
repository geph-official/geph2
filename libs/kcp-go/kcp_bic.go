package kcp

import "log"

const bicMultiplier = 1

func (kcp *KCP) bic_onloss(lost []uint32) {
	maxRun := 1
	currRun := 0
	lastSeen := uint32(0)
	for _, v := range lost {
		if v == lastSeen+1 {
			currRun++
			if maxRun < currRun {
				maxRun = currRun
			}
		} else {
			currRun = 0
		}
		lastSeen = v
	}
	// log.Println("lost with maxRun", maxRun)
	// if maxRun < int(kcp.cwnd/20) || maxRun < 10 {
	// 	return
	// }
	beta := 0.25 / bicMultiplier
	if kcp.cwnd < kcp.wmax {
		kcp.wmax = kcp.cwnd * (2.0 - beta) / 2.0
	} else {
		kcp.wmax = kcp.cwnd
	}
	kcp.cwnd = kcp.cwnd * (1.0 - beta)
}

func (kcp *KCP) bic_onack(acks int32) {
	if doLogging {
		log.Printf("BIC cwnd=%v // t=%.2f%%", int(kcp.cwnd),
			100*float64(kcp.retrans)/float64(kcp.trans))
	}
	// // TCP BIC
	for i := 0; i < int(acks*bicMultiplier); i++ {
		var bicinc float64
		if kcp.cwnd < kcp.wmax {
			bicinc = (kcp.wmax - kcp.cwnd) / 2
		} else {
			bicinc = kcp.cwnd - kcp.wmax
		}
		if bicinc <= 1 {
			bicinc = 1
		} else {
			if bicinc > 64 {
				bicinc = 64
			}
		}
		kcp.cwnd += bicinc / kcp.cwnd
		if uint32(kcp.cwnd) > kcp.rmt_wnd {
			kcp.cwnd = float64(kcp.rmt_wnd)
		}
	}
	// if doLogging {
	// 	log.Printf("BIC cwnd %.2f => %.2f [%.2f %%]", kcp.cwnd, kcp.wmax, float64(kcp.retrans)/float64(kcp.trans)*100)
	// }
	// kcp.cwnd += float64(acks) * kcp.aimd_multiplier()
}
