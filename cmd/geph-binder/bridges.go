package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/geph-official/geph2/libs/kcp-go"
	"github.com/geph-official/geph2/libs/niaucchi4"
	"github.com/patrickmn/go-cache"
)

// cache of all bridge info. string => bridgeInfo
var bridgeCache = cache.New(time.Hour, time.Hour)

type bridgeInfo struct {
	Cookie   []byte
	Host     string
	LastSeen time.Time
}

func addBridge(nfo bridgeInfo) {
	bridgeCache.SetDefault(string(nfo.Cookie), nfo)
}

// cache of bridge *mappings*. string => []string
var bridgeMapCache = cache.New(time.Hour*48, time.Hour)

func getBridges(id string) []string {
	if mapping, ok := bridgeMapCache.Get(id); ok {
		return mapping.([]string)
	}
	itms := bridgeCache.Items()
	seed := fmt.Sprintf("%v-%v", id, time.Now())
	probability := 10.0 / float64(len(itms))
	var toret []string
	for k := range itms {
		h := sha256.Sum256([]byte(k + seed))
		num := binary.BigEndian.Uint32(h[:4])
		if float64(num) < probability*float64(4294967295) {
			toret = append(toret, k)
		}
	}
	bridgeMapCache.SetDefault(id, toret)
	return toret
}

func handleGetBridges(w http.ResponseWriter, r *http.Request) {
	// TODO validate the ticket
	bridges := getBridges(r.RemoteAddr)
	w.Header().Set("content-type", "application/json")
	var laboo []bridgeInfo
	for _, str := range bridges {
		if val, ok := bridgeCache.Get(str); ok {
			laboo = append(laboo, val.(bridgeInfo))
		}
	}
	json.NewEncoder(w).Encode(laboo)
}

func handleAddBridge(w http.ResponseWriter, r *http.Request) {
	// first get the cookie
	cookie, err := hex.DecodeString(r.FormValue("cookie"))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	// check the cookie
	ok, err := checkBridge(cookie)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if !ok {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	bi := bridgeInfo{
		Cookie:   cookie,
		Host:     r.FormValue("host"),
		LastSeen: time.Now(),
	}
	if !testBridge(bi) {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	// add the bridge
	addBridge(bi)
}

func testBridge(bi bridgeInfo) bool {
	udpsock, err := net.ListenPacket("udp", ":")
	if err != nil {
		panic(err)
	}
	defer udpsock.Close()
	e2e := niaucchi4.E2EListen(niaucchi4.ObfsListen(bi.Cookie, udpsock))
	if err != nil {
		panic(err)
	}
	defer e2e.Close()
	kcp, err := kcp.NewConn(bi.Host, nil, 0, 0, e2e)
	if err != nil {
		panic(err)
	}
	defer kcp.Close()
	kcp.SetDeadline(time.Now().Add(time.Second * 10))
	start := time.Now()
	rlp.Encode(kcp, "ping")
	var resp string
	rlp.Decode(kcp, &resp)
	if resp != "ping" {
		log.Println(bi.Host, "failed ping test!")
		return false
	}
	log.Println(bi.Host, "passed ping test in", time.Since(start))
	return true
}
