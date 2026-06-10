package service

import (
	"sync"
)

// EventKind identifies the type of an SSE event.
type EventKind string

const (
	EventNodeStatus EventKind = "node_status"
	EventJobStatus  EventKind = "job_status"
	EventRunStatus  EventKind = "run_status"
)

// Event is a single message published to an engagement's SSE stream.
type Event struct {
	Kind         EventKind
	EngagementID string
	Data         any
}

// subscriber is a single SSE client subscription.
type subscriber struct {
	ch chan Event
}

// Hub is a per-engagement publish/subscribe hub for SSE events. It is safe for
// concurrent use. Slow subscribers are dropped after their buffer fills.
type Hub struct {
	mu   sync.RWMutex
	subs map[string][]*subscriber // keyed by engagement ID
}

// NewHub returns an empty Hub.
func NewHub() *Hub {
	return &Hub{subs: make(map[string][]*subscriber)}
}

// Subscribe returns a channel that will receive events for engagementID.
// The caller must call Unsubscribe when done to release resources.
func (h *Hub) Subscribe(engagementID string) (<-chan Event, func()) {
	s := &subscriber{ch: make(chan Event, 32)}
	h.mu.Lock()
	h.subs[engagementID] = append(h.subs[engagementID], s)
	h.mu.Unlock()

	unsub := func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		subs := h.subs[engagementID]
		for i, existing := range subs {
			if existing == s {
				h.subs[engagementID] = append(subs[:i], subs[i+1:]...)
				close(s.ch)
				return
			}
		}
	}
	return s.ch, unsub
}

// Publish sends an event to all current subscribers for the engagement. Slow
// subscribers that have a full buffer are skipped (non-blocking send).
func (h *Hub) Publish(e Event) {
	h.mu.RLock()
	subs := h.subs[e.EngagementID]
	h.mu.RUnlock()

	for _, s := range subs {
		select {
		case s.ch <- e:
		default:
			// Slow subscriber: drop the event rather than block.
		}
	}
}
