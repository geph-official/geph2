package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"database/sql"
	"fmt"
	"time"

	lru "github.com/hashicorp/golang-lru"
	"github.com/nullchinchilla/natrium"
	"golang.org/x/crypto/ed25519"
)

var pgDB *sql.DB

// checkBridgeKey checks whether a bridge cookie is allowed.
func checkBridgeKey(key string) (ok bool, err error) {
	tx, err := pgDB.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback()
	var cookieCount uint
	err = tx.QueryRow("select count(key) from bridgekeys where key = $1",
		key).Scan(&cookieCount)
	if err != nil {
		return
	}
	ok = cookieCount > 0
	err = tx.Commit()
	return
}

// getTicketIdentity returns the RSA ticket identity for a particular account class.
func getTicketIdentity(tier string) (sk *rsa.PrivateKey, err error) {
	tx, err := pgDB.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback()
	// different entries for each tier
	tableKey := fmt.Sprintf("ticket-id-%v", tier)
	var skBts []byte
	err = tx.QueryRow("select value from secrets where key = $1", tableKey).Scan(&skBts)
	if err == sql.ErrNoRows {
		err = nil
		sk, _ = rsa.GenerateKey(rand.Reader, 2048)
		skBts = x509.MarshalPKCS1PrivateKey(sk)
		if err != nil {
			panic(err)
		}
		_, err = tx.Exec("insert into secrets values ($1, $2)", tableKey, skBts)
		err = tx.Commit()
		return
	}
	if err != nil {
		return
	}
	sk, err = x509.ParsePKCS1PrivateKey(skBts)
	if err != nil {
		panic(err)
	}
	err = tx.Commit()
	return
}

// getMasterIdentity returns the ed25519 master identity.
func getMasterIdentity() (sk ed25519.PrivateKey, err error) {
	tx, err := pgDB.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback()
	err = tx.QueryRow("select value from secrets where key = $1", "master-id").Scan(&sk)
	if err == sql.ErrNoRows {
		err = nil
		_, sk, _ = ed25519.GenerateKey(nil)
		_, err = tx.Exec("insert into secrets values ($1, $2)", "master-id", sk)
		if err != nil {
			return
		}
		err = tx.Commit()
		return
	}
	err = tx.Commit()
	return
}

var hashCache, _ = lru.New(65536)

// verifyUser verifies a username/password by looking up the database. uid < 0 means authentication failed.
func verifyUser(uname, pwd string) (uid int, subExpiry time.Time, paytx map[time.Time]int, err error) {
	tx, err := pgDB.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback()
	row := tx.QueryRow("SELECT ID, PwdHash FROM Users WHERE Username = $1", uname)
	var PwdHash string
	err = row.Scan(&uid, &PwdHash)
	if err == sql.ErrNoRows {
		uid = -1
		err = nil
		return
	}
	if err != nil {
		return
	}
	if v, ok := hashCache.Get(pwd); ok && v.(string) == PwdHash {
		//log.Println("password of", uname, "found in cache")
	} else {
		if !natrium.PasswordVerify([]byte(pwd), PwdHash) {
			uid = -1
			return
		}
		hashCache.Add(pwd, PwdHash)
	}
	// check subscription
	err = tx.QueryRow("select expires from subscriptions where id = $1", uid).Scan(&subExpiry)
	if err == sql.ErrNoRows {
		err = nil
	} else if err != nil {
		return
	} else {
		var rows *sql.Rows
		// loop over the invoices
		rows, err = tx.Query("select createtime, amount from invoices where id=$1 and paid='t'", uid)
		if err != nil {
			return
		}
		paytx = make(map[time.Time]int)
		for rows.Next() {
			var key time.Time
			var val int
			err = rows.Scan(&key, &val)
			if err != nil {
				return
			}
			paytx[key] = val
		}
	}
	// TODO actually verify the pwd hash
	err = tx.Commit()
	return
}

// createUser creates a username/password pair.
func createUser(uname, pwd string) (err error) {
	tx, err := pgDB.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback()
	hpwd := natrium.PasswordHash([]byte(pwd), 5, 32*1024*1024)
	_, err = tx.Exec("insert into users (username, pwdhash, freebalance, createtime) values ($1, $2, $3, $4)",
		uname, hpwd, 10000, time.Now())
	if err != nil {
		return
	}
	err = tx.Commit()
	return
}
