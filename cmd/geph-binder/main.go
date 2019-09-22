package main

import (
	"crypto/x509"
	"database/sql"
	"log"
	"net/http"

	"github.com/gorilla/mux"
	_ "github.com/lib/pq"
)

func main() {
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
	tpkf, err := getTicketIdentity("free")
	if err != nil {
		log.Fatal("cannot obtain free ticket identity", err)
	}
	log.Printf("Geph2 binder started")
	log.Printf("MPK      = %x", sk.Public())
	log.Printf("TPK-free = %x", x509.MarshalPKCS1PublicKey(&tpkf.PublicKey))

	r := mux.NewRouter()
	r.HandleFunc("/get-ticket", handleGetTicket)
	r.HandleFunc("/add-bridge", handleAddBridge)
	r.HandleFunc("/get-bridges", handleGetBridges)
	if err := http.ListenAndServe(":9080", r); err != nil {
		panic(err)
	}
}
