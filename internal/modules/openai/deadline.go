package openai

import (
	"strconv"
	"time"
)

// Optional client budget bounds upstream work even after client disconnects.
func requestBudget(raw string) time.Duration {
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds < 1 || seconds > 300 {
		return 5 * time.Minute
	}
	return time.Duration(seconds) * time.Second
}
