package main

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/cryptoballot/rsablind"
	"github.com/geph-official/geph2/libs/bdclient"
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
	_, expiry, err := verifyUser(r.FormValue("user"), r.FormValue("pwd"))
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

func handleGetTicket(w http.ResponseWriter, r *http.Request) {
	// first authenticate
	uid, expiry, err := verifyUser(r.FormValue("user"), r.FormValue("pwd"))
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
	log.Println("get-ticket: verified user", r.FormValue("user"), "as expiry", expiry)
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
	log.Println("get-ticket: user", r.FormValue("user"), "sent us blinded of length", len(blinded))
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
	toWrite.Ticket = ticket
	toWrite.PaidExpiry = expiry
	b, err := json.Marshal(toWrite)
	if err != nil {
		panic(err)
	}
	w.Write([]byte(b))
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
