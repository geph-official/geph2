package kcp

import (
	"log"
	"math"
)

func (kcp *KCP) bic_onloss(lost int) {
	// if doLogging {
	// 	log.Println("loss detected for BIC")
	// }
	kcp.shortLoss *= math.Pow(0.999, float64(lost))
	kcp.longLoss *= math.Pow(0.9999, float64(lost))
	if kcp.cwnd > 8 {
		if kcp.shortLoss < kcp.longLoss/1.1 {
			kcp.shortLoss = 1.0
			beta := 0.1
			if kcp.cwnd < kcp.wmax {
				kcp.wmax = kcp.cwnd * (2.0 - beta) / 2.0
			} else {
				kcp.wmax = kcp.cwnd
			}
			kcp.cwnd = kcp.cwnd * (1.0 - beta)
		}
	}
}

func (kcp *KCP) aimd_multiplier() float64 {
	return kcp.DRE.minRtt / 50.0
}

func (kcp *KCP) bic_onack(acks int32) {
	// // TCP BIC
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
		kcp.cwnd += bicinc / kcp.cwnd
		if uint32(kcp.cwnd) > kcp.rmt_wnd {
			kcp.cwnd = float64(kcp.rmt_wnd)
		}
		bicinc *= kcp.aimd_multiplier()
	}
	// if doLogging {
	// 	log.Printf("BIC cwnd %.2f => %.2f [%.2f %%]", kcp.cwnd, kcp.wmax, float64(kcp.retrans)/float64(kcp.trans)*100)
	// }
	// kcp.cwnd += float64(acks) * kcp.aimd_multiplier()
	if doLogging {
		log.Printf("cwnd=%v // s=%.2f%% // l=%.2f%% // t=%.2f%%", int(kcp.cwnd),
			kcp.shortLoss*100,
			kcp.longLoss*100,
			float64(kcp.retrans)/float64(kcp.trans)*100)
	}
}
