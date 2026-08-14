package cluster

import (
	"testing"

	"github.com/litesql/go-ha"
)

type stubLeader struct {
	target string
	ready  chan struct{}
}

func (s stubLeader) IsLeader() bool         { return s.target == "" }
func (s stubLeader) Ready() chan struct{}   { return s.ready }
func (s stubLeader) RedirectTarget() string { return s.target }

func TestSetLeaderIsVisibleToLeaderProvider(t *testing.T) {
	f := &Feature{}
	if f.LeaderProvider() != nil {
		t.Fatal("expected nil leader before set")
	}
	var lp ha.LeaderProvider = stubLeader{target: "http://leader:8090", ready: make(chan struct{})}
	f.setLeader(lp)
	got := f.LeaderProvider()
	if got == nil {
		t.Fatal("expected leader after set")
	}
	if got.RedirectTarget() != "http://leader:8090" {
		t.Fatalf("target = %q", got.RedirectTarget())
	}
	if got.IsLeader() {
		t.Fatal("non-empty target is not the leader")
	}
}
