package kcp

import "log"

func (kcp *KCP) bic_onloss() {
	if doLogging {
		log.Println("loss detected for BIC")
	}
	beta := 0.05
	if kcp.cwnd < kcp.wmax {
		kcp.wmax = kcp.cwnd * (2.0 - beta) / 2.0
	} else {
		kcp.wmax = kcp.cwnd
	}
	kcp.cwnd = kcp.cwnd * (1.0 - beta)
	bdp := kcp.bdp() / float64(kcp.mss)
	if kcp.cwnd < bdp {
		kcp.cwnd = bdp
		if doLogging {
			log.Println("setting cwnd to bdp", int(kcp.bdp()))
		}
	}
}

func (kcp *KCP) bic_onack(acks int32) {
	// TCP BIC
	for i := 0; i < int(acks); i++ {
		var bicinc float64
		if kcp.cwnd < kcp.wmax {
			bicinc = (kcp.wmax - kcp.cwnd) / 2
		} else {
			bicinc = kcp.cwnd - kcp.wmax
		}
		if bicinc <= 1 {
			bicinc = 1
		} else {
			if bicinc > 512 {
				bicinc = 512
			}
		}
		if kcp.DRE.minRtt > 20 {
			bicinc *= kcp.DRE.minRtt / 20
		}
		kcp.cwnd += bicinc / kcp.cwnd
		if uint32(kcp.cwnd) > kcp.rmt_wnd {
			kcp.cwnd = float64(kcp.rmt_wnd)
		}
	}
	if doLogging {
		log.Printf("BIC cwnd %.2f => %.2f [%.2f %%]", kcp.cwnd, kcp.wmax, float64(kcp.retrans)/float64(kcp.trans)*100)
	}
}
