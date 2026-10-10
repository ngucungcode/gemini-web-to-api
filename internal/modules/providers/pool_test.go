package providers

import (
	"context"
	"fmt"
	"testing"

	"go.uber.org/zap"
)

func TestPoolSkipsUnhealthy(t *testing.T) {
	a := &Client{name: "a", log: zap.NewNop(), stopRefresh: make(chan struct{})}
	b := &Client{name: "b", log: zap.NewNop(), stopRefresh: make(chan struct{})}
	a.healthy = false
	b.healthy = true
	p := NewPool([]*Client{a, b}, zap.NewNop())
	for i := 0; i < 5; i++ {
		if got := p.Next(); got != b {
			t.Fatalf("want healthy b, got %v", got.AccountName())
		}
	}
}

func TestPoolRoundRobin(t *testing.T) {
	a := &Client{name: "a", log: zap.NewNop(), stopRefresh: make(chan struct{})}
	b := &Client{name: "b", log: zap.NewNop(), stopRefresh: make(chan struct{})}
	a.healthy = true
	b.healthy = true
	p := NewPool([]*Client{a, b}, zap.NewNop())
	seen := map[string]int{}
	for i := 0; i < 4; i++ {
		seen[p.Next().AccountName()]++
	}
	if seen["a"] != 2 || seen["b"] != 2 {
		t.Fatalf("want round robin, got %v", seen)
	}
}

func TestPoolAddRemove(t *testing.T) {
	p := NewPool(nil, zap.NewNop())
	if p.Size() != 0 {
		t.Fatal("want empty pool")
	}
	c := &Client{name: "x", log: zap.NewNop(), stopRefresh: make(chan struct{})}
	p.Add(c)
	if p.Size() != 1 || p.Get("x") != c {
		t.Fatal("add failed")
	}
	if p.Remove("x") != c || p.Size() != 0 {
		t.Fatal("remove failed")
	}
}

func TestPoolNoClients(t *testing.T) {
	p := NewPool(nil, zap.NewNop())
	if p.Next() != nil {
		t.Fatal("want nil client")
	}
	if _, err := p.GenerateContent(context.Background(), "hi"); err == nil {
		t.Fatal("want error with no clients")
	}
	if p.IsHealthy() {
		t.Fatal("empty pool must not be healthy")
	}
}

func TestBackoffDurations(t *testing.T) {
	if backoffFor(fmt.Errorf("429 quota")) < backoffFor(fmt.Errorf("boom")) {
		t.Fatal("quota errors should back off longer")
	}
}
