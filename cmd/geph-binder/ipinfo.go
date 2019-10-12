package main

import (
	"encoding/json"
	"net/http"

	"github.com/abh/geoip"
)

var db *geoip.GeoIP

func init() {
	var e error
	db, e = geoip.Open("/usr/share/GeoIP/GeoIP.dat")
	if e != nil {
		panic(e)
	}
}

func handleClientInfo(w http.ResponseWriter, r *http.Request) {
	var cinfo struct {
		Address string
		Country string
	}
	addr := r.Header.Get("X-Forwarded-For")
	cinfo.Address = addr
	w.Header().Add("content-type", "application/json")

	country, _ := db.GetCountry(addr)
	cinfo.Country = country
	json.NewEncoder(w).Encode(cinfo)
}
