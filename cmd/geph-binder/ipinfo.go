package main

import (
	"encoding/json"
	"net/http"
	"strings"

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
	countUserAgent(r)
	var cinfo struct {
		Address string
		Country string
	}
	addrs := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	addr := addrs[0]
	cinfo.Address = addr
	w.Header().Add("content-type", "application/json")

	country, _ := db.GetCountry(addr)
	cinfo.Country = country
	// if cinfo.Country == "IR" {
	// 	cinfo.Country = "CN" // HACK to put iranian users on bridges
	// }
	json.NewEncoder(w).Encode(cinfo)
}
