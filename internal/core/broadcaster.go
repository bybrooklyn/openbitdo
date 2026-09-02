package core

import "sync"

// broadcaster is a minimal multi-subscriber fan-out for FirmwareProgressEvent,
// standing in for tokio::sync::broadcast::Sender/Receiver: each subscriber
// gets its own buffered channel receiving every event published after it
// subscribed. A slow subscriber that fills its buffer has the oldest queued
// event dropped for it rather than blocking the publisher — progress events
// are inherently best-effort/latest-matters, so dropping stale ones instead
// of stalling the firmware transfer loop is the correct tradeoff.
type broadcaster struct {
	mu   sync.Mutex
	subs map[chan FirmwareProgressEvent]struct{}
}

func newBroadcaster() *broadcaster {
	return &broadcaster{subs: make(map[chan FirmwareProgressEvent]struct{})}
}

func (b *broadcaster) subscribe() chan FirmwareProgressEvent {
	ch := make(chan FirmwareProgressEvent, 128)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *broadcaster) publish(event FirmwareProgressEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- event:
		default:
			// Buffer full: drop the oldest queued event to make room rather
			// than blocking the transfer loop on a slow/absent subscriber.
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- event:
			default:
			}
		}
	}
}
