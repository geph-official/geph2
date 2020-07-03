package main

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cryptoballot/rsablind"
	"github.com/geph-official/geph2/libs/bdclient"
	"github.com/patrickmn/go-cache"
)

func handleGetTicketKey(w http.ResponseWriter, r *http.Request) {
	// check type
	key, err := getTicketIdentity(r.FormValue("tier"))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Write([]byte(base64.RawStdEncoding.EncodeToString(x509.MarshalPKCS1PublicKey(&key.PublicKey))))
}

func handleGetTier(w http.ResponseWriter, r *http.Request) {
	// first authenticate
	_, expiry, _, err := verifyUser(r.FormValue("user"), r.FormValue("pwd"))
	if err != nil {
		log.Println("cannot verify user:", err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if expiry.After(time.Now()) {
		w.Write([]byte("paid"))
	} else {
		w.Write([]byte("free"))
	}
}

var cpuSemaphore chan bool

func init() {
	cpuSemaphore = make(chan bool, runtime.GOMAXPROCS(0)*2)
}

var limiterCache sync.Map

var goodIPCache = cache.New(time.Hour, time.Minute)

func handleGetTicket(w http.ResponseWriter, r *http.Request) {
	// first authenticate
	uid, expiry, paytx, err := verifyUser(r.FormValue("user"), r.FormValue("pwd"))
	if err != nil {
		log.Println("cannot verify user:", err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if uid < 0 {
		log.Println("cannot log in user:", r.FormValue("user"))
		w.WriteHeader(http.StatusForbidden)
		return
	}
	// ticketLimiter, _ := limiterCache.LoadOrStore(uid, rate.NewLimiter(rate.Every(time.Minute*4), 100))
	// if !ticketLimiter.(*rate.Limiter).Allow() {
	// 	log.Println("*** VIOLATED LIMIT ", r.FormValue("user"))
	// 	time.Sleep(time.Second * 10)
	// 	w.WriteHeader(http.StatusTooManyRequests)
	// 	return
	// }
	log.Println("verified", r.FormValue("user"))
	//log.Println("get-ticket: verified user", r.FormValue("user"), "as expiry", expiry)
	var tier string
	if expiry.After(time.Now()) {
		tier = "paid"
	} else {
		tier = "free"
	}
	blinded, err := base64.RawStdEncoding.DecodeString(r.FormValue("blinded"))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	//log.Println("get-ticket: user", r.FormValue("user"), "sent us blinded of length", len(blinded))
	// get the key
	key, err := getTicketIdentity(tier)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	// issue the ticket. TODO rate limit this
	ticket, err := rsablind.BlindSign(key, blinded)
	if err != nil {
		panic(err)
	}
	var toWrite bdclient.TicketResp
	toWrite.Tier = tier
	toWrite.Ticket = ticket
	toWrite.PaidExpiry = expiry
	if paytx != nil {
		for k, v := range paytx {
			toWrite.Transactions = append(toWrite.Transactions, bdclient.PaymentTx{
				Amount: v,
				Date:   k,
			})
		}
		sort.Slice(toWrite.Transactions, func(i, j int) bool {
			return toWrite.Transactions[i].Date.After(toWrite.Transactions[j].Date)
		})
	}
	b, err := json.Marshal(toWrite)
	if err != nil {
		panic(err)
	}
	w.Write([]byte(b))
	id := strings.Split(r.Header.Get("X-Forwarded-For"), ",")[0]
	goodIPCache.SetDefault(id, uid)
}

func handleRedeemTicket(w http.ResponseWriter, r *http.Request) {
	// check type
	if tier := r.FormValue("tier"); tier != "free" && tier != "paid" {
		log.Println("bad tier:", tier)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	// obtain key
	key, err := getTicketIdentity(r.FormValue("tier"))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	// obtain values
	ubmsg, err := base64.RawStdEncoding.DecodeString(r.FormValue("ubmsg"))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	ubsig, err := base64.RawStdEncoding.DecodeString(r.FormValue("ubsig"))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	// verify
	if err := rsablind.VerifyBlindSignature(&key.PublicKey, ubmsg, ubsig); err != nil {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	return
}
