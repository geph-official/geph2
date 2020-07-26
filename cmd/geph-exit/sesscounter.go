package main

import (
	"time"

	"github.com/patrickmn/go-cache"
)

// session counter by using a forgetful cache. counting the elements gives us the session count.
var freeSessCounter = cache.New(time.Minute*10, time.Second*10)
var paidSessCounter = cache.New(time.Minute*10, time.Second*10)
