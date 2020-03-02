package main

import (
	"io/ioutil"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/elazarl/goproxy"
	"github.com/geph-official/geph2/cmd/geph-client/chinalist"
	"github.com/geph-official/geph2/libs/cwl"
	"github.com/geph-official/geph2/libs/tinysocks"
	"golang.org/x/time/rate"
)

func listenStats() {
	log.Infoln("STATS on", statsAddr)
	// spin up stats server
	statsMux := http.NewServeMux()
	statsServ := &http.Server{
		Addr:         statsAddr,
		Handler:      statsMux,
		ReadTimeout:  time.Minute,
		WriteTimeout: time.Minute,
	}
	statsMux.HandleFunc("/proxy.pac", handleProxyPac)
	statsMux.HandleFunc("/kill", handleKill)
	statsMux.HandleFunc("/", handleStats)
	statsMux.HandleFunc("/logs", handleLogs)
	statsMux.HandleFunc("/debugpack", handleDebugPack)
	statsMux.HandleFunc("/stacktrace", handleStacktrace)
	err := statsServ.ListenAndServe()
	if err != nil {
		panic(err)
	}
}

func listenHTTP() {
	log.Infoln("HTTP on", httpAddr)
	// HTTP proxy
	srv := goproxy.NewProxyHttpServer()
	srv.Tr = &http.Transport{
		Dial: func(n, d string) (net.Conn, error) {
			conn, err := dialTun(d)
			if err != nil {
				return nil, err
			}
			conn.(*net.TCPConn).SetKeepAlive(false)
			conn.(*net.TCPConn).SetWriteBuffer(16384)
			return conn, nil
		},
		IdleConnTimeout: time.Second * 60,
		Proxy:           nil,
	}
	blankLogger := log.New()
	blankLogger.SetOutput(ioutil.Discard)
	srv.Logger = blankLogger
	proxServ := &http.Server{
		Addr:        httpAddr,
		Handler:     srv,
		ReadTimeout: time.Minute * 5,
		IdleTimeout: time.Minute * 5,
	}
	err := proxServ.ListenAndServe()
	if err != nil {
		panic(err.Error())
	}
}

func listenSocks() {
	listener, err := net.Listen("tcp", socksAddr)
	if err != nil {
		panic(err)
	}
	semaphore := make(chan bool, 512)
	downLimit := rate.NewLimiter(rate.Inf, 10000000)
	upLimit := rate.NewLimiter(rate.Inf, 1000*1000)
	useStats(func(sc *stats) {
		if sc.Tier == "free" {
			upLimit = rate.NewLimiter(100*1000, 1000*1000)
		}
	})
	log.Infoln("SOCKS5 on", socksAddr)
	for {
		cl, err := listener.Accept()
		if err != nil {
			panic(err)
		}
		go func() {
			defer cl.Close()
			cl.(*net.TCPConn).SetKeepAlive(false)
			cl.(*net.TCPConn).SetReadBuffer(16384)
			select {
			case semaphore <- true:
				defer func() {
					<-semaphore
				}()
			default:
				return
			}
			cmd, rmAddrProt, err := tinysocks.ReadRequest(cl)
			if err != nil {
				log.Debugln("Bad SOCKS5 request:", err)
				return
			}
			rmAddr := rmAddrProt.String()
			host, port, err := net.SplitHostPort(rmAddr)
			if cmd == tinysocks.CmdBind {
				log.Debugln("Unsupported command:", cmd)
				tinysocks.CompleteRequestTCP(7, cl)
				return
			} else if cmd == tinysocks.CmdUDPAssociate {
				if value, err := strconv.Atoi(port); (value != 53 && interceptDNS) || err != nil {
					log.Debugln("Unsupported command:", cmd)
					tinysocks.CompleteRequestTCP(7, cl)
				} else {
					listenAddr := tinysocks.ParseAddr(cl.(net.Conn).LocalAddr().String())
					_, err = cl.Write(append([]byte{5, 0, 0}, listenAddr...)) // SOCKS v5, reply succeeded
					if err != nil {
						log.Debugln("Unsupported command:", cmd)
						return
					}
					buf := make([]byte, 1)
					// block here
					for {
						_, err := cl.Read(buf)
						if err, ok := err.(net.Error); ok && err.Timeout() {
							continue
						}
						//log.Debugln("UDP Associate End.")
						return
					}
				}
				return
			} else if cmd != tinysocks.CmdConnect {
				log.Debugln("Unsupported command:", cmd)
				tinysocks.CompleteRequestTCP(7, cl)
			}
			if realName := fakeIPToName(host); realName != "" {
				rmAddr = net.JoinHostPort(realName, port)
				host = realName
			}
			var remote net.Conn
			var ok bool
			if bypassChinese && bypassHost(host) {
				remote, err = net.Dial("tcp", rmAddr)
				if err != nil {
					log.Printf("[%v] failed to bypass %v", len(semaphore), remote)
					tinysocks.CompleteRequestTCP(5, cl)
					return
				}
				log.Debugf("[%v] BYPASSED %v", len(semaphore), rmAddr)
				remote.(*net.TCPConn).SetKeepAlive(false) // app responsibility
			} else {
				start := time.Now()
				remote, ok = sWrap.DialCmd("proxy", rmAddr)
				if !ok {
					return
				}
				defer remote.Close()
				ping := time.Since(start)
				log.Debugf("[%v] opened tcp %v in %v", len(semaphore), rmAddr, ping)
				useStats(func(sc *stats) {
					pmil := ping.Milliseconds()
					if time.Since(sc.PingTime).Seconds() > 30 || uint64(pmil) < sc.MinPing {
						sc.MinPing = uint64(pmil)
						sc.PingTime = time.Now()
					}
				})
				if !ok {
					tinysocks.CompleteRequestTCP(5, cl)
					return
				}
			}
			tinysocks.CompleteRequestTCP(0, cl)
			go func() {
				defer remote.Close()
				defer cl.Close()
				cwl.CopyWithLimit(remote, cl, upLimit, func(n int) {
					useStats(func(sc *stats) {
						sc.UpBytes += uint64(n)
					})
				}, time.Hour)
			}()
			cwl.CopyWithLimit(cl, remote,
				downLimit, func(n int) {
					useStats(func(sc *stats) {
						sc.DownBytes += uint64(n)
					})
				}, time.Hour)
		}()
	}
}

const udpBufSize = 64 * 1024

func listenSocksUDP() {
	c, err := net.ListenPacket("udp", socksAddr)
	if err != nil {
		log.Debugf("UDP local listen error: %v", err)
		return
	}
	defer c.Close()

	nm := newNATmap(UDPTimeout)
	buf := make([]byte, udpBufSize)

	for {
		n, raddr, err := c.ReadFrom(buf)
		if err != nil {
			log.Debugf("UDP local read error: %v", err)
			continue
		}

		tgtAddr := tinysocks.SplitAddr(buf[3:n])
		if tgtAddr == nil {
			log.Debugf("failed to split target address from packet: %q", buf[:n])
			continue
		}

		pc := nm.Get(raddr.String())
		if pc == nil {
			pc, err = net.ListenPacket("udp", "")
			if err != nil {
				log.Debugf("UDP local listen error: %v", err)
				continue
			}
			nm.Add(raddr, c, pc, tgtAddr.String())
		}

		tgtUDPAddr, err := net.ResolveUDPAddr("udp", dnsAddr)
		if err != nil {
			log.Debugf("failed to resolve target UDP address: %v", err)
			continue
		}

		payload := buf[3+len(tgtAddr) : n]

		_, err = pc.WriteTo(payload, tgtUDPAddr) // accept only UDPAddr despite the signature
		if err != nil {
			log.Debugf("UDP remote write error: %v", err)
			continue
		}
	}
}

// Packet NAT table
type natmap struct {
	sync.RWMutex
	m       map[string]net.PacketConn
	timeout time.Duration
}

func newNATmap(timeout time.Duration) *natmap {
	m := &natmap{}
	m.m = make(map[string]net.PacketConn)
	m.timeout = timeout
	return m
}

func (m *natmap) Get(key string) net.PacketConn {
	m.RLock()
	defer m.RUnlock()
	return m.m[key]
}

func (m *natmap) Set(key string, pc net.PacketConn) {
	m.Lock()
	defer m.Unlock()

	m.m[key] = pc
}

func (m *natmap) Del(key string) net.PacketConn {
	m.Lock()
	defer m.Unlock()

	pc, ok := m.m[key]
	if ok {
		delete(m.m, key)
		return pc
	}
	return nil
}

func (m *natmap) Add(peer net.Addr, dst, src net.PacketConn, tgt string) {
	m.Set(peer.String(), src)

	go func() {
		timedCopy(dst, peer, src, m.timeout, tgt)
		if pc := m.Del(peer.String()); pc != nil {
			pc.Close()
		}
	}()
}

// copy from src to dst at target with read timeout
func timedCopy(dst net.PacketConn, target net.Addr, src net.PacketConn, timeout time.Duration, tgt string) error {
	buf := make([]byte, udpBufSize)
	for {
		src.SetReadDeadline(time.Now().Add(timeout))
		n, _, err := src.ReadFrom(buf)
		if err != nil {
			return err
		}
		//srcAddr := tinysocks.ParseAddr(raddr.String())
		srcAddr := tinysocks.ParseAddr(tgt)

		copy(buf[len(srcAddr):], buf[:n])
		copy(buf, srcAddr)
		_, err = dst.WriteTo(append([]byte{0, 0, 0}, buf[:len(srcAddr)+n]...), target)

		if err != nil {
			return err
		}
	}
}

// TODO bypass local domains
func bypassHost(str string) bool {
	return chinalist.IsChinese(str)
}
