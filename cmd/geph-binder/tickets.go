package main

import (
	"encoding/base64"
	"log"
	"net/http"

	"github.com/cryptoballot/rsablind"
)

func handleGetTicket(w http.ResponseWriter, r *http.Request) {
	// check type
	if tier := r.FormValue("tier"); tier != "free" && tier != "paid" {
		log.Println("bad tier:", tier)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	// first authenticate
	uid, err := verifyUser(r.FormValue("user"), r.FormValue("pwd"))
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
	log.Println("get-ticket: verified user", r.FormValue("user"), "as uid", uid)
	blinded, err := base64.RawStdEncoding.DecodeString(r.FormValue("blinded"))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	// get the key
	key, err := getTicketIdentity(r.FormValue("tier"))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	// issue the ticket. TODO rate limit this
	ticket, err := rsablind.BlindSign(key, blinded)
	if err != nil {
		panic(err)
	}
	w.Write([]byte(base64.RawStdEncoding.EncodeToString(ticket)))
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
