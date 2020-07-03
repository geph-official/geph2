package main

import (
	"database/sql"
	"log"
	"net"
	"net/http"
	"time"

	statsd "github.com/etsy/statsd/examples/go"
	"github.com/gorilla/mux"
	_ "github.com/lib/pq"
)

func rotateTickets() {
	for {
		endOfDay := time.Now().Truncate(time.Hour * 24).Add(time.Hour * 24)
		log.Println("rotateTickets() sleeping until", endOfDay)
		time.Sleep(time.Until(endOfDay))
		log.Println("rotateTickets() actually rotating!")
		go func() {
			tx, err := pgDB.Begin()
			if err != nil {
				log.Println(err)
				return
			}
			defer tx.Rollback()
			tx.Exec("delete from secrets where key = $1 or key = $2",
				"ticket-id-free", "ticket-id-paid")
			tx.Commit()
		}()
	}
}

var statClient *statsd.StatsdClient

func main() {
	z, e := net.ResolveUDPAddr("udp", "c2.geph.io:8125")
	if e != nil {
		panic(e)
	}
	statClient = statsd.New(z.IP.String(), z.Port)
	var err error
	pgDB, err = sql.Open("postgres",
		"postgres://postgres:postgres@localhost/postgres?sslmode=disable")
	if err != nil {
		log.Fatal("cannot connect to database:", err)
	}
	sk, err := getMasterIdentity()
	if err != nil {
		log.Fatal("cannot obtain master identity:", err)
	}
	go rotateTickets()
	log.Printf("Geph2 binder started")
	log.Printf("MPK      = %x", sk.Public())

	r := mux.NewRouter()
	r.HandleFunc("/get-ticket", handleGetTicket)
	r.HandleFunc("/get-tier", handleGetTier)
	r.HandleFunc("/get-ticket-key", handleGetTicketKey)
	r.HandleFunc("/redeem-ticket", handleRedeemTicket)
	r.HandleFunc("/add-bridge", handleAddBridge)
	r.HandleFunc("/get-bridges", handleGetBridges)
	r.HandleFunc("/client-info", handleClientInfo)
	r.HandleFunc("/captcha", handleCaptcha)
	r.HandleFunc("/register", handleRegister)
	r.HandleFunc("/warpfronts", handleGetWarpfronts)
	//r.HandleFunc("/cryptrr", handleCryptrr)
	if err := http.ListenAndServe(":9080", r); err != nil {
		panic(err)
	}
}
