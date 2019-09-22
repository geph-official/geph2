package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

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
	// obtain the ticket
	ticket := r.FormValue("ticket")
	// TODO validate the ticket
	bridges := getBridges(ticket)
	w.Header().Set("content-type", "application/json")
	json.NewEncoder(w).Encode(bridges)
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
	// add the bridge
	addBridge(bridgeInfo{
		Cookie:   cookie,
		Host:     r.FormValue("host"),
		LastSeen: time.Now(),
	})
}
