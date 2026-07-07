package session

import (
	"sync"

	"github.com/Ken-Chy129/agent-master/internal/store"
)

// Delta is a live-only, token-level assistant text fragment. Deltas are NOT
// written to the ledger (they are ephemeral UX); history and resume rely on the
// committed assistant_message event instead.
type Delta struct {
	RunID string `json:"runId"`
	Text  string `json:"text"`
	Index int    `json:"index"`
}

// Frame is what a subscriber receives: either a committed ledger event (has a
// seq, drives resume) or a live delta (ephemeral, no seq).
type Frame struct {
	SessionID string
	Event     *store.Event
	Delta     *Delta
}

// broadcaster fans frames out to live SSE subscribers, keyed by session id.
type broadcaster struct {
	mu   sync.Mutex
	next int
	subs map[string]map[int]chan Frame
}

func newBroadcaster() *broadcaster {
	return &broadcaster{subs: make(map[string]map[int]chan Frame)}
}

// subscribe registers a live listener for a session. The returned channel is
// buffered; unsubscribe when done.
func (b *broadcaster) subscribe(sessionID string) (int, <-chan Frame) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.next++
	id := b.next
	ch := make(chan Frame, 256)
	if b.subs[sessionID] == nil {
		b.subs[sessionID] = make(map[int]chan Frame)
	}
	b.subs[sessionID][id] = ch
	return id, ch
}

func (b *broadcaster) unsubscribe(sessionID string, id int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if m := b.subs[sessionID]; m != nil {
		if ch, ok := m[id]; ok {
			close(ch)
			delete(m, id)
		}
		if len(m) == 0 {
			delete(b.subs, sessionID)
		}
	}
}

// publish delivers a frame to all live subscribers of a session. A subscriber
// whose buffer is full is dropped (committed events recover on reconnect via
// after_seq; a dropped delta is harmless — the committed message still arrives).
func (b *broadcaster) publish(f Frame) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for id, ch := range b.subs[f.SessionID] {
		select {
		case ch <- f:
		default:
			close(ch)
			delete(b.subs[f.SessionID], id)
		}
	}
}
