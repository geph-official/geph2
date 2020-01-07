package kcppp

import "time"

func currentMS() uint32 {
	return uint32(time.Now().UnixNano() / 1000000)
}

type rtAction struct {
	dline  time.Time
	action func()
}

type resettableTimer struct {
	actions chan rtAction
}

func newTimer() *resettableTimer {
	rt := &resettableTimer{
		actions: make(chan rtAction, 32),
	}
	go func() {
		var dline time.Time
		var action func()
		for {
			select {
			case <-time.After(dline.Sub(time.Now())):
				if action != nil {
					action()
				}
				dline = time.Now().Add(time.Hour * 200000)
			case act, ok := <-rt.actions:
				if !ok {
					return
				}
				dline = act.dline
				action = act.action
			}
		}
	}()
	return rt
}
