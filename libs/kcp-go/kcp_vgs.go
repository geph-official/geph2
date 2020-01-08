package kcp

import "log"

func (kcp *KCP) vgs_onack(acks int32) {
	expected := float64(kcp.mss) * kcp.cwnd / (float64(kcp.DRE.minRtt) / 1000)
	actual := kcp.DRE.maxAckRate
	alpha := actual * 1
	beta := actual * 3
	diff := expected - actual

	if doLogging {
		log.Printf("cwnd = %v, expected = %.2fK; actual = %.2fK; loss= %.2f%%", int(kcp.cwnd), expected/1000, actual/1000,
			100*float64(kcp.retrans)/float64(kcp.trans))
	}
	if diff < alpha {
		kcp.cwnd += float64(acks) * kcp.DRE.minRtt / 4 / kcp.cwnd
	} else if diff > beta {
		kcp.cwnd -= float64(acks) * kcp.DRE.minRtt / 4 / kcp.cwnd
	}
	if kcp.cwnd < 4 {
		kcp.cwnd = 4
	}
}
