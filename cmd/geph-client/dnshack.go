package main

import (
	_ "unsafe"

	log "github.com/sirupsen/logrus"
)

// DNS hacks for Android and other systems without /etc/resolv.conf

// this only work before any Lookup call and net.dnsReadConfig() failed
//go:linkname defaultNS net.defaultNS
var defaultNS []string

func setDefaultNS2(addrs []string) {
	defaultNS = addrs
}

func hackDNS() {
	if bypassChinese {
		log.Println("using 114.114.114.114 as hacked DNS because we want to bypass Chinese")
		setDefaultNS2([]string{"114.114.114.114:53"})
	} else {
		log.Println("hack DNS for lack of /etc/resolv.conf")
		setDefaultNS2([]string{"74.82.42.42:53", "1.0.0.1:53", "8.8.8.8:53", "8.8.4.4:53"})
	}
}
