package niaucchi4

const rwSize = 1024

type replayWindow struct {
	lastBuf [rwSize]uint64
	highest uint64
}
