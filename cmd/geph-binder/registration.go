package main

import (
	"encoding/json"
	"net/http"

	"github.com/dchest/captcha"
)

func handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		return
	}
	var req struct {
		Username    string
		Password    string
		CaptchaID   string
		CaptchaSoln string
	}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	// check captcha
	if !captcha.VerifyString(req.CaptchaID, req.CaptchaSoln) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	// register
	err = createUser(req.Username, req.Password)
	if err != nil {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	// ok!
	return
}

func handleCaptcha(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("content-type", "image/png")
	w.Header().Add("cache-control", "no-cache")
	id := captcha.NewLen(8)
	w.Header().Add("x-captcha-id", id)
	captcha.WriteImage(w, id, 200, 100)
}
