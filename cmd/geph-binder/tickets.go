package main

import (
	"encoding/hex"
	"log"
	"net/http"

	"github.com/cryptoballot/rsablind"
)

func handleGetTicket(w http.ResponseWriter, r *http.Request) {
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
	blinded, err := hex.DecodeString(r.FormValue("blinded"))
	if err != nil {
		log.Println("can't decode blinded:", err.Error())
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	// get the key
	key, err := getTicketIdentity("free")
	if err != nil {
		log.Println("cannot obtain key:", err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	// issue the ticket. TODO rate limit this
	ticket, err := rsablind.BlindSign(key, blinded)
	if err != nil {
		panic(err)
	}
	w.Write([]byte(hex.EncodeToString(ticket)))
}

func handleRedeemTicket(w http.ResponseWriter, r *http.Request)
