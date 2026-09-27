package queue

import (
	"sync"
	"testing"
	"time"
)

// LockDestination must serialize writers per path while letting other
// paths proceed independently.
func TestLockDestinationSerializes(t *testing.T) {
	unlock := LockDestination("test-path")
	second := make(chan struct{})
	go func() {
		unlock2 := LockDestination("test-path")
		close(second)
		unlock2()
	}()
	select {
	case <-second:
		t.Fatal("second locker entered while gate held")
	case <-time.After(100 * time.Millisecond):
	}
	other := make(chan struct{})
	go func() {
		unlock3 := LockDestination("other-path")
		close(other)
		unlock3()
	}()
	select {
	case <-other:
	case <-time.After(2 * time.Second):
		t.Fatal("independent path blocked")
	}
	unlock()
	select {
	case <-second:
	case <-time.After(2 * time.Second):
		t.Fatal("second locker never entered after unlock")
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); u := LockDestination("race"); u() }()
	}
	wg.Wait()
}
