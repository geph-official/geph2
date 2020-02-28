package kcp

import "log"

const multiplier = 16

func (kcp *KCP) vgs_onack(acks int32) {
	factor := float64(kcp.mss) / (float64(kcp.DRE.minRtt) / 1000)
	expected := kcp.cwnd * factor
	actual := kcp.DRE.maxAckRate
	alpha := factor * 128
	beta := factor * 256
	diff := expected - actual

	if doLogging {
		log.Printf("cwnd = %v, expected = %.2fK; actual = %.2fK; loss= %.2f%%", int(kcp.cwnd), expected/1000, actual/1000,
			100*float64(kcp.retrans)/float64(kcp.trans))
	}
	if diff < alpha {
		kcp.cwnd += (multiplier * float64(acks) * kcp.DRE.minRtt / 4 / kcp.cwnd)
	} else if diff > beta {
		kcp.cwnd -= float64(acks) * kcp.DRE.minRtt / 4 / kcp.cwnd
	}
	if kcp.cwnd < 4 {
		kcp.cwnd = 4
	}
}
