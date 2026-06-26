// Package events is an in-process pub/sub bus that feeds the SSE endpoint.
// Producers (the sync runner, OAuth callback) publish events; the SSE handler
// subscribes and forwards them to the browser, optionally scoped to one account.
package events

import (
	"sync"
)

// Event is a single notification streamed to clients.
type Event struct {
	Type        string `json:"type"`                   // log | status | auth-ok | operation
	AccountID   int64  `json:"account_id,omitempty"`   // account the event concerns
	SourceUser  string `json:"source_user,omitempty"`  // for display
	DestGmail   string `json:"dest_gmail,omitempty"`   // auth-ok: destination that gained tokens
	OperationID string `json:"operation_id,omitempty"` // RFC3339 ts identifying the run
	Line        string `json:"line,omitempty"`         // log: an imapsync output line
	RSSBytes    int64  `json:"rss_bytes,omitempty"`    // log/operation: peak child RSS in bytes
	Status      string `json:"status,omitempty"`       // status: idle|running|ok|failed|skipped|stopped
	Reason      string `json:"reason,omitempty"`       // status: why it was skipped/failed
	Timestamp   string `json:"timestamp,omitempty"`    // RFC3339
}

// Bus is a fan-out bus. Subscribers receive a buffered channel; if a subscriber
// is slow the oldest events are dropped (non-blocking send) so a stalled client
// never blocks the sync runner.
type Bus struct {
	mu   sync.Mutex
	subs map[chan Event]struct{}
}

// New creates an empty bus.
func New() *Bus {
	return &Bus{subs: make(map[chan Event]struct{})}
}

// Subscribe returns a channel of events and a cancel func that unsubscribes.
func (b *Bus) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 256)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	cancel := func() {
		b.mu.Lock()
		if _, ok := b.subs[ch]; ok {
			delete(b.subs, ch)
			close(ch)
		}
		b.mu.Unlock()
	}
	return ch, cancel
}

// Publish fans an event out to all subscribers (non-blocking).
func (b *Bus) Publish(e Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- e:
		default:
			// drop: subscriber is behind
		}
	}
}
