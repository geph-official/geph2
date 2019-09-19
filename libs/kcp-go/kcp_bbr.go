package kcp

// func (kcp *KCP) bbrOnConnectionInit() {
// 	kcp.bbrInit()
// }

// func (kcp *KCP) bbrInit() {
// 	BBR := &kcp.BBR
// 	BBR.rtProp = math.NaN()
// 	BBR.rtProp = time.Now()
// 	kcp.bbrInitRoundCounting()
// 	kcp.bbrInitFullPipe()
// 	kcp.bbrInitPacingRate()
// 	kcp.bbrEnterStartup()
// }

// func (kcp *KCP) bbrEnterStartup() {
// 	kcp.BBR.state = "startup"
// 	bbrHighGain := 2.89
// 	kcp.BBR.pacing_gain = bbrHighGain
// 	kcp.BBR.cwnd_gain = bbrHighGain
// }

// func (kcp *KCP) bbrUpdateOnAck() {
// 	kcp.bbrUpdateModelAndState()
// 	kcp.bbrUpdateControlParameters()
// }

// func (kcp *KCP) bbrUpdateModelAndState() {
// 	kcp.bbrUpdateBtlBw()
// 	kcp.bbrCheckCyclePhase()
// 	kcp.bbrCheckFullPipe()
// 	kcp.bbrCheckDrain()
// 	kcp.bbrUpdateRtProp()
// 	kcp.bbrCheckProbeRTT()
// }

// func (kcp *KCP) bbrUpdateControlParameters() {
// 	kcp.bbrSetPacingRate()
// 	kcp.bbrSetSendQuantum()
// 	kcp.bbrSetCwnd()
// }
