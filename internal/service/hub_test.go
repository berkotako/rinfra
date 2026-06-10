package service_test

import (
	"sync"
	"testing"

	"github.com/rinfra/rinfra/internal/service"
)

// TestHub_ConcurrentPublishUnsubscribe stresses the Hub with simultaneous
// publishers and subscribers churning. Before the fix, Publish copied the
// subscriber slice and released the lock before sending, so an Unsubscribe
// could close a channel mid-send and panic with "send on closed channel".
// Run with -race to also catch the data race.
func TestHub_ConcurrentPublishUnsubscribe(t *testing.T) {
	h := service.NewHub()
	const eng = "ENG-1"

	var wg sync.WaitGroup

	// Publishers.
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 2000; j++ {
				h.Publish(service.Event{Kind: service.EventNodeStatus, EngagementID: eng, Data: j})
			}
		}()
	}

	// Subscribers that repeatedly subscribe, drain a bit, and unsubscribe.
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				ch, unsub := h.Subscribe(eng)
				select {
				case <-ch:
				default:
				}
				unsub()
			}
		}()
	}

	wg.Wait()
}
