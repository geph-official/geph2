package cryptrr

// // Client encapsulates a CryptRR client transport.
// type Client interface {
// 	// RoundTrip sends a CiphMsg and returns a CiphMsg response.
// 	RoundTrip(CiphMsg) (CiphMsg error)
// }

// // DomainFrontClient is an implementation of Client that uses POST requests to a domain-fronted endpoint
// type DomainFrontClient struct {
// 	// Endpoint should include the "fake" domain, for example https://ajax.aspnetcdn.com/
// 	Endpoint string
// 	// RealHost is the real host to put in the Host header, for example gephbinder.azureedge.net
// 	RealHost string

// 	once    sync.Once
// 	hclient *http.Client
// }

// func (dft *DomainFrontClient) init() {
// 	dft.once.Do(func() {
// 		dft.hclient = &http.Client{
// 			Transport: &http.Transport{
// 				Proxy:           nil,
// 				IdleConnTimeout: time.Second * 10,
// 			},
// 			Timeout: time.Second * 10,
// 		}
// 	})
// }

// // RoundTrip impl
// func (dft *DomainFrontClient) RoundTrip(req CiphMsg) (resp CiphMsg, err error) {
// 	dft.init()

//}
