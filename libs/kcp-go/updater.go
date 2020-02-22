package kcp

import (
	"container/heap"
	"sync"
	"time"

	"gopkg.in/tomb.v1"
)

// entry contains a session update info
type entry struct {
	ts time.Time
	s  *UDPSession
}

// a global heap managed kcp.flush() caller
type updateHeap struct {
	entries  []entry
	exists   map[*UDPSession]bool
	mu       sync.Mutex
	chWakeUp chan struct{}
	onzuru   func()
	stop     tomb.Tomb
}

func (h *updateHeap) Len() int           { return len(h.entries) }
func (h *updateHeap) Less(i, j int) bool { return h.entries[i].ts.Before(h.entries[j].ts) }
func (h *updateHeap) Swap(i, j int) {
	h.entries[i], h.entries[j] = h.entries[j], h.entries[i]
	h.entries[i].s.updaterIdx = i
	h.entries[j].s.updaterIdx = j
}

func (h *updateHeap) Push(x interface{}) {
	h.entries = append(h.entries, x.(entry))
	n := len(h.entries)
	h.entries[n-1].s.updaterIdx = n - 1
}

func (h *updateHeap) Pop() interface{} {
	n := len(h.entries)
	x := h.entries[n-1]
	h.entries[n-1].s.updaterIdx = -1
	h.entries[n-1] = entry{} // manual set nil for GC
	h.entries = h.entries[0 : n-1]
	return x
}

func (h *updateHeap) init(onzuru func()) {
	h.chWakeUp = make(chan struct{}, 1)
	h.exists = make(map[*UDPSession]bool)
	h.onzuru = onzuru
}

func (h *updateHeap) addSession(s *UDPSession) {
	h.mu.Lock()
	heap.Push(h, entry{time.Now(), s})
	h.exists[s] = true
	h.mu.Unlock()
	h.wakeup()
}

func (h *updateHeap) addSessionIfNotExists(s *UDPSession) {
	h.mu.Lock()
	if !h.exists[s] {
		heap.Push(h, entry{time.Now(), s})
		h.exists[s] = true
	}
	h.mu.Unlock()
	h.wakeup()
}

func (h *updateHeap) removeSession(s *UDPSession) {
	h.mu.Lock()
	if s.updaterIdx != -1 {
		heap.Remove(h, s.updaterIdx)
		delete(h.exists, s)
	}
	h.mu.Unlock()
}

func (h *updateHeap) wakeup() {
	select {
	case h.chWakeUp <- struct{}{}:
	default:
	}
}

func (h *updateHeap) updateTask() {
	timer := time.NewTimer(0)
	for {
		select {
		case <-timer.C:
		case <-h.chWakeUp:
		case <-h.stop.Dying():
			return
		}
		h.mu.Lock()
		hlen := h.Len()
		for i := 0; i < hlen; i++ {
			entry := &h.entries[0]
			now := time.Now()
			if !now.Before(entry.ts) {
				zuru := now.Sub(entry.ts)
				if zuru.Milliseconds() > 10 {
					//log.Printf("WARNING!! %p zuru %v", h, zuru)
					h.onzuru()
				}
				interval := entry.s.update()
				if interval != 0 {
					entry.ts = time.Now().Add(interval)
					heap.Fix(h, 0)
				} else {
					delete(h.exists, entry.s)
					heap.Fix(h, 0)
					heap.Remove(h, 0)
					hlen--
				}
			} else {
				break
			}
		}

		if hlen > 0 {
			timer.Reset(h.entries[0].ts.Sub(time.Now()))
		}
		h.mu.Unlock()
	}
}
