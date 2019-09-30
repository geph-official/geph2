package bdclient

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"

	"github.com/cryptoballot/rsablind"
)

// Client represents a binder client.
type Client struct {
	hclient     *http.Client
	frontDomain string
	realDomain  string
}

// NewClient creates a new domain-fronting binder client with the given frontDomain and realDomain. frontDomain should start with `https://`.
func NewClient(frontDomain, realDomain string) *Client {
	return &Client{
		hclient: &http.Client{
			Transport: &http.Transport{
				DisableKeepAlives: true,
				Proxy:             nil,
			},
		},
		frontDomain: frontDomain,
		realDomain:  realDomain,
	}
}

func badStatusCode(s int) error {
	return fmt.Errorf("unexpected status code %v", s)
}

// AddBridge uploads some bridge info.
func (cl *Client) AddBridge(cookie []byte, host string) (err error) {
	req, _ := http.NewRequest("GET", fmt.Sprintf("%v/add-bridge?cookie=%x&host=%v", cl.frontDomain, cookie, host), bytes.NewReader(nil))
	req.Host = cl.realDomain
	resp, err := cl.hclient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		err = badStatusCode(resp.StatusCode)
	}
	return
}

// GetTicketKey obtains the remote ticketing key.
// TODO caching, gossip?
func (cl *Client) GetTicketKey(tier string) (tkey *rsa.PublicKey, err error) {
	req, _ := http.NewRequest("GET", fmt.Sprintf("%v/get-ticket-key?tier=%v", cl.frontDomain, tier), bytes.NewReader(nil))
	req.Host = cl.realDomain
	resp, err := cl.hclient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		err = badStatusCode(resp.StatusCode)
		return
	}
	b64key, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return
	}
	btskey, err := base64.RawStdEncoding.DecodeString(string(b64key))
	if err != nil {
		return
	}
	tkey, err = x509.ParsePKCS1PublicKey(btskey)
	return
}

// GetTier gets the tier of a user.
func (cl *Client) GetTier(username, password string) (tier string, err error) {
	v := url.Values{}
	v.Set("user", username)
	v.Set("pwd", password)
	req, _ := http.NewRequest("GET", fmt.Sprintf("%v/get-tier?%v", cl.frontDomain, v.Encode()), bytes.NewReader(nil))
	req.Host = cl.realDomain
	resp, err := cl.hclient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return
	}
	tier = string(b)
	return
}

// TicketResp is the response for ticket getting
type TicketResp struct {
	Ticket       []byte
	Tier         string
	PaidExpiry   time.Time
	Transactions []PaymentTx
}

// PaymentTx is a payment in USD cents.
type PaymentTx struct {
	Date   time.Time
	Amount int
}

// GetTicket obtains an authentication ticket.
func (cl *Client) GetTicket(username, password string) (ubmsg, ubsig []byte, details TicketResp, err error) {
	// First get ticket key
	fkey, err := cl.GetTicketKey("free")
	if err != nil {
		return
	}
	pkey, err := cl.GetTicketKey("paid")
	if err != nil {
		return
	}
	// Pick
	tier, err := cl.GetTier(username, password)
	if err != nil {
		return
	}
	var tkey *rsa.PublicKey
	if tier == "free" {
		tkey = fkey
	} else {
		tkey = pkey
	}
	// Create our ticketing request
	unblinded := make([]byte, 1536/8)
	rand.Read(unblinded)
	blinded, unblinder, err := rsablind.Blind(tkey, unblinded)
	if err != nil {
		panic(err)
	}
	// Obtain the ticket
	v := url.Values{}
	v.Set("user", username)
	v.Set("pwd", password)
	v.Set("blinded", base64.RawStdEncoding.EncodeToString(blinded))
	req, _ := http.NewRequest("GET", fmt.Sprintf("%v/get-ticket?%v", cl.frontDomain, v.Encode()), bytes.NewReader(nil))
	req.Host = cl.realDomain
	resp, err := cl.hclient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	var respDec TicketResp
	err = json.NewDecoder(resp.Body).Decode(&respDec)
	if err != nil {
		return
	}
	// unblind the ticket
	ubsig = rsablind.Unblind(tkey, respDec.Ticket, unblinder)
	ubmsg = unblinded
	details = respDec
	return
}

// BridgeInfo describes a bridge
type BridgeInfo struct {
	Cookie   []byte
	Host     string
	LastSeen time.Time
}

// GetBridges obtains a set of bridges.
func (cl *Client) GetBridges(ubmsg, ubsig []byte) (bridges []BridgeInfo, err error) {
	req, _ := http.NewRequest("GET", fmt.Sprintf("%v/get-bridges", cl.frontDomain), bytes.NewReader(nil))
	req.Host = cl.realDomain
	resp, err := cl.hclient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		err = badStatusCode(resp.StatusCode)
		return
	}
	err = json.NewDecoder(resp.Body).Decode(&bridges)
	return
}

// RedeemTicket redeems a ticket.
func (cl *Client) RedeemTicket(tier string, ubmsg, ubsig []byte) (err error) {
	// Obtain the ticket
	v := url.Values{}
	v.Set("ubmsg", base64.RawStdEncoding.EncodeToString(ubmsg))
	v.Set("ubsig", base64.RawStdEncoding.EncodeToString(ubsig))
	v.Set("tier", tier)
	req, _ := http.NewRequest("GET", fmt.Sprintf("%v/redeem-ticket?%v", cl.frontDomain, v.Encode()), bytes.NewReader(nil))
	req.Host = cl.realDomain
	resp, err := cl.hclient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		err = badStatusCode(resp.StatusCode)
		return
	}
	return
}
