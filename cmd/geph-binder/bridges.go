package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/geph-official/geph2/libs/cshirt2"
	"github.com/patrickmn/go-cache"
)

// cache of all bridge info. string => bridgeInfo
var bridgeCache = cache.New(time.Minute*2, time.Minute)

type bridgeInfo struct {
	Cookie     []byte
	Host       string
	LastSeen   time.Time
	AllocGroup string
}

func addBridge(nfo bridgeInfo) {
	bridgeCache.SetDefault(string(nfo.Cookie), nfo)
}

// cache of bridge *mappings*. string => []string
var bridgeMapCache = cache.New(time.Hour, time.Hour)

func getBridges(id string) []string {
	if mapping, ok := bridgeMapCache.Get(id); ok {
		return mapping.([]string)
	}
	itms := bridgeCache.Items()
	seed := fmt.Sprintf("%v-%v", id, time.Now())
	probability := 10.0 / float64(len(itms))
	candidates := make([][]string, 10)
	candidateWeights := make([]int, 10)
	// try 10 times
	for i := 0; i < 10; i++ {
		for k := range itms {
			h := sha256.Sum256([]byte(k + seed))
			num := binary.BigEndian.Uint32(h[:4])
			if float64(num) < probability*float64(4294967295) || true {
				candidates[i] = append(candidates[i], k)
			}
			//TODO diversity
			candidateWeights[i] = len(candidates[i])
		}
	}
	var toret []string
	trweight := -1
	for i := 0; i < 10; i++ {
		if candidateWeights[i] > trweight {
			trweight = candidateWeights[i]
			toret = candidates[i]
		}
	}
	// shuffle
	rand.Shuffle(len(toret), func(i, j int) {
		toret[i], toret[j] = toret[j], toret[i]
	})
	bridgeMapCache.SetDefault(id, toret)
	return toret
}

func handleGetWarpfronts(w http.ResponseWriter, r *http.Request) {
	host2front, err := getWarpfronts()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("content-type", "application/json")
	json.NewEncoder(w).Encode(host2front)
}

var counterCache = cache.New(time.Minute, time.Hour)

func handleGetBridges(w http.ResponseWriter, r *http.Request) {
	id := strings.Split(r.Header.Get("X-Forwarded-For"), ",")[0]
	// result, _ := goodIPCache.Get(id)
	// if result == nil {
	// 	log.Println("BAD IP:", id)
	// 	w.WriteHeader(http.StatusForbidden)
	// 	return
	// }
	// id = fmt.Sprintf("%v", result)
	// fmt.Println("good IP with id", id)

	isEphemeral := r.FormValue("type") == "ephemeral"
	if isEphemeral {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	// TODO validate the ticket
	bridges := getBridges(id)
	w.Header().Set("content-type", "application/json")
	idhash := sha256.Sum256([]byte(id))
	w.Header().Set("X-Requestor-ID", hex.EncodeToString(idhash[:]))
	seenAGs := make(map[string]bool)
	var laboo []bridgeInfo
	vacate := func() {
		bridgeMapCache.Delete(id)
	}
	for _, str := range bridges {
		vali, ok := bridgeCache.Get(str)
		if ok {
			val := vali.(bridgeInfo)
			if !seenAGs[val.AllocGroup] {
				seenAGs[val.AllocGroup] = true
				hostPort := strings.Split(val.Host, ":")
				if len(hostPort) == 2 {
					val.Host = fmt.Sprintf("%v.sslip.io:%v", strings.Replace(hostPort[0], ".", "-", -1), hostPort[1])
					laboo = append(laboo, val)
				}
			}
		} else {
			vacate()
			return
		}
	}
	if len(laboo) == 0 {
		vacate()
		return
	}
	json.NewEncoder(w).Encode(laboo)
}

func handleAddBridge(w http.ResponseWriter, r *http.Request) {
	// first get the cookie
	cookie, err := hex.DecodeString(r.FormValue("cookie"))
	if err != nil {
		log.Println("can't add bridge (bad cookie)")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	// check the cookie
	_, pwd, _ := r.BasicAuth()
	ok, err := checkBridgeKey(pwd)
	if err != nil {
		log.Println("can't add bridge (bad DB)")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if !ok {
		log.Printf("can't add bridge (bad bridge key %v)", pwd)
		w.WriteHeader(http.StatusForbidden)
		return
	}
	bi := bridgeInfo{
		Cookie:     cookie,
		Host:       r.FormValue("host"),
		LastSeen:   time.Now(),
		AllocGroup: r.FormValue("allocGroup"),
	}
	// add the bridge
	addBridge(bi)
}

func testBridge(bi bridgeInfo) bool {
	rawconn, err := net.Dial("tcp", bi.Host)
	if err != nil {
		log.Println("bridge test failed for", bi.Host, err)
		return false
	}
	defer rawconn.Close()
	realconn, err := cshirt2.ClientLegacy(bi.Cookie, rawconn)
	if err != nil {
		log.Println("bridge test failed for", bi.Host, err)
		return false
	}
	defer realconn.Close()
	realconn.SetDeadline(time.Now().Add(time.Second * 10))
	start := time.Now()
	rlp.Encode(realconn, "ping")
	var resp string
	rlp.Decode(realconn, &resp)
	if resp != "ping" {
		log.Println(bi.Host, "failed ping test!")
		return false
	}
	log.Println(bi.Host, "passed ping test in", time.Since(start))
	return true
}
