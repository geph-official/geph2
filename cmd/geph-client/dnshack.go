package main

import _ "unsafe"

// DNS hacks for Android and other systems without /etc/resolv.conf

// this only work before any Lookup call and net.dnsReadConfig() failed
//go:linkname defaultNS net.defaultNS
var defaultNS []string

func setDefaultNS2(addrs []string) {
	defaultNS = addrs
}

func init() {
	setDefaultNS2([]string{"74.82.42.42:53", "1.0.0.1:53", "8.8.8.8:53", "8.8.4.4:53"})
}
