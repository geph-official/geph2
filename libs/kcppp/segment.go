package kcppp

const (
	cmdPUSH = 81
	cmdACK  = 82
	cmdWASK = 83
	cmdWINS = 84
	cmdSACK = 91
	cmdRST  = 0
)

type inlineBytes struct {
	len int
	raw [2048]byte
}

func (ib *inlineBytes) slice() []byte {
	return ib.raw[:ib.len]
}

type segHeader struct {
	ConvID    uint32
	Cmd       uint8
	Rsrv      uint8
	Window    uint16
	Timestamp uint32
	Seqno     uint32
	Ackno     uint32
}

type segment struct {
	Header segHeader
	Body   inlineBytes
}
