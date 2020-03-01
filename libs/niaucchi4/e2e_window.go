package niaucchi4

const rwSize = 128

type replayWindow struct {
	lastBuf []uint64
	bufPtr  uint8
	highest uint64
}

func (rw *replayWindow) add(val uint64) {
	// add
	if len(rw.lastBuf) < rwSize {
		rw.lastBuf = append(rw.lastBuf, val)
	} else {
		rw.lastBuf[rw.bufPtr] = val
		rw.bufPtr++
		rw.bufPtr %= rwSize
	}
}

func (rw *replayWindow) check(val uint64) bool {
	if val+rwSize < rw.highest {
		return false
	}
	if val > rw.highest {
		// obviously not replay
		rw.highest = val
		rw.add(val)
		return true
	}
	// check if already exists
	for _, v := range rw.lastBuf {
		if v == val {
			return false
		}
	}
	rw.add(val)
	return true
}
