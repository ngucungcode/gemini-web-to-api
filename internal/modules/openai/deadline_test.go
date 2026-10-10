package openai

import (
	"testing"
	"time"
)

func TestRequestBudget(t *testing.T) {
	for _, v := range []struct {
		s    string
		want time.Duration
	}{{"", 5 * time.Minute}, {"80", 80 * time.Second}, {"0", 5 * time.Minute}, {"-1", 5 * time.Minute}, {"9999", 5 * time.Minute}, {"bad", 5 * time.Minute}} {
		if got := requestBudget(v.s); got != v.want {
			t.Fatalf("%q: %v", v.s, got)
		}
	}
}
