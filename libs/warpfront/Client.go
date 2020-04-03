package warpfront

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"time"

	"golang.org/x/time/rate"
)

func getWithHost(client *http.Client, url string, host string) (resp *http.Response, err error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return
	}
	req.Host = host
	return client.Do(req)
}

func postWithHost(client *http.Client, url string, host string, body io.Reader) (resp *http.Response, err error) {
	req, err := http.NewRequest("POST", url, body)
	if err != nil {
		return
	}
	req.Host = host
	req.Header.Add("Content-Type", "application/octet-stream")
	return client.Do(req)
}

// Connect returns a warpfront session connected to the given front and real host. The front must contain a protocol scheme (http:// or https://).
func Connect(client *http.Client, frontHost string, realHost string) (net.Conn, error) {
	// generate session number
	num := make([]byte, 32)
	rand.Read(num)
	// register our session
	resp, err := getWithHost(client, fmt.Sprintf("%v/register?id=%x", frontHost, num), realHost)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bad status code: %v", resp.StatusCode)
	}
	sesh := newSession()

	emptyGetLimiter := rate.NewLimiter(1, 10)

	go func() {
		defer sesh.Close()
		// poll and stuff into rx
		for i := 0; ; i++ {
			resp, err := getWithHost(client,
				fmt.Sprintf("%v/%x?serial=%v", frontHost, num, i),
				realHost)
			if err != nil {
				return
			}
			if resp.StatusCode != http.StatusOK {
				log.Println("WAT DIE")
				resp.Body.Close()
				return
			}
			totpkts := 0
			for {
				lbts := make([]byte, 4)
				_, err := io.ReadFull(resp.Body, lbts)
				if err != nil {
					resp.Body.Close()
					return
				}
				if binary.BigEndian.Uint32(lbts) == 0 {
					//log.Println("warpfront: client got continuation signal, looping around")
					resp.Body.Close()
					goto OUT
				}
				totpkts++
				buf := make([]byte, binary.BigEndian.Uint32(lbts))
				_, err = io.ReadFull(resp.Body, buf)
				if err != nil {
					resp.Body.Close()
					return
				}
				select {
				case sesh.rx <- buf:
				case <-sesh.ded:
					resp.Body.Close()
					return
				}
				if err != nil {
					resp.Body.Close()
					return
				}
			}
		OUT:
			if totpkts == 0 {
				emptyGetLimiter.Wait(context.Background())
			}
		}
	}()
	go func() {
		defer sesh.Close()
		// drain something from tx
		timer := time.NewTicker(time.Millisecond * 250)
		buff := bytes.NewBuffer(nil)
		for i := 0; ; i++ {
			select {
			case <-timer.C:
				if buff.Len() > 0 {
					resp, err := postWithHost(client,
						fmt.Sprintf("%v/%x?serial=%v", frontHost, num, i),
						realHost,
						buff)
					if err != nil {
						return
					}
					io.Copy(ioutil.Discard, resp.Body)
					resp.Body.Close()
				}
			case bts := <-sesh.tx:
				buff.Write(bts)
			case <-sesh.ded:
				timer.Stop()
				return
			}
		}
	}()

	// couple closing the session with deletion
	go func() {
		<-sesh.ded
		getWithHost(client, fmt.Sprintf("%v/delete?id=%x", frontHost, num), realHost)
	}()

	// return the sesh
	return sesh, nil
}
