package tinysocks

import (
	"errors"
	"fmt"
	"io"
)

func CompleteRequest(errcode byte, conn io.ReadWriteCloser) error {
	scratch := make([]byte, 128)
	scratch[0] = 0x05
	scratch[1] = errcode
	scratch[2] = 0x00
	scratch[3] = 0x01
	scratch[4] = 0x00
	scratch[5] = 0x00
	scratch[6] = 0x00
	scratch[7] = 0x00
	scratch[8] = 0x00
	scratch[9] = 0x00

	_, err := conn.Write(scratch[:10])
	if err != nil {
		return errors.New("Couldn't complete handshake")
	}
	return nil
}

func ReadRequest(conn io.ReadWriteCloser) (string, error) {
	scratch := make([]byte, 128)
	_, err := io.ReadFull(conn, scratch[:2])
	if err != nil {
		return "", err
	}
	if scratch[0] != 5 {
		return "", errors.New("SOCKS version mismatch")
	}
	i := scratch[1]
	_, err = io.ReadFull(conn, scratch[:i])
	if err != nil {
		return "", errors.New("Couldn't read auths")
	}
	scratch[0] = 0x05
	scratch[1] = 0x00
	_, err = conn.Write(scratch[:2])
	if err != nil {
		return "", errors.New("Couldn't write auths")
	}
	_, err = io.ReadFull(conn, scratch[:4])
	if err != nil {
		return "", errors.New("Couldn't read conn type")
	}
	conntype := scratch[3]
	var thing []byte
	var toret string
	if conntype == 0x01 {
		_, err = io.ReadFull(conn, scratch[:4])
		if err != nil {
			return "", errors.New("Couldn't read IP address")
		}
		thing = make([]byte, 4)
		copy(thing, scratch)
		toret = fmt.Sprintf("%d.%d.%d.%d", scratch[0], scratch[1],
			scratch[2], scratch[3])
	} else if conntype == 0x03 {
		_, err = io.ReadFull(conn, scratch[:1])
		if err != nil {
			return "", errors.New("Couldn't read domain length")
		}
		length := scratch[0]
		_, err = io.ReadFull(conn, scratch[:length])
		if err != nil {
			return "", errors.New("Couldn't read domain")
		}
		thing = make([]byte, length+1)
		thing[0] = byte(length)
		copy(thing[1:], scratch)
		toret = string(thing[1:])
	} else {
		return "", errors.New("conntype mismatch")
	}
	_, err = io.ReadFull(conn, scratch[:2])
	if err != nil {
		return "", errors.New("Couldn't read port")
	}
	portnum := int(scratch[0])*256 + int(scratch[1])
	toret = fmt.Sprintf("%s:%d", toret, portnum)
	return toret, nil
}
