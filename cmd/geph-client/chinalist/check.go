package chinalist

import "github.com/weppos/publicsuffix-go/publicsuffix"

// IsChinese checks if a FQDN is Chinese.
func IsChinese(fqdn string) bool {
	dom, err := publicsuffix.Domain(fqdn)
	if err != nil {
		return false
	}
	return domainList[fqdn] || domainList[dom]
}
