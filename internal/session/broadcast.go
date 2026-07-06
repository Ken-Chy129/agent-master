package session

import (
	"sync"

	"github.com/Ken-Chy129/agent-master/internal/store"
)

// broadcaster fans committed ledger events out to live SSE subscribers,
// keyed by session id.
type broadcaster struct {
	mu   sync.Mutex
	next int
	subs map[string]map[int]chan store.Event
}

func newBroadcaster() *broadcaster {
	return &broadcaster{subs: make(map[string]map[int]chan store.Event)}
}

// subscribe registers a live listener for a session. The returned channel is
// buffered; unsubscribe when done.
func (b *broadcaster) subscribe(sessionID string) (int, <-chan store.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.next++
	id := b.next
	ch := make(chan store.Event, 256)
	if b.subs[sessionID] == nil {
		b.subs[sessionID] = make(map[int]chan store.Event)
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

// publish delivers an event to all live subscribers of a session. A subscriber
// whose buffer is full is dropped (it will recover by reconnecting with
// after_seq and replaying from the ledger).
func (b *broadcaster) publish(e store.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for id, ch := range b.subs[e.SessionID] {
		select {
		case ch <- e:
		default:
			close(ch)
			delete(b.subs[e.SessionID], id)
		}
	}
}
