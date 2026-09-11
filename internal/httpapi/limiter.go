package httpapi

import (
	"sync"
	"time"
)

type attempt struct {
	count int
	reset time.Time
}

type loginLimiter struct {
	mu       sync.Mutex
	attempts map[string]attempt
}

func newLoginLimiter() *loginLimiter { return &loginLimiter{attempts: make(map[string]attempt)} }

func (l *loginLimiter) Allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	current := l.attempts[key]
	if now.After(current.reset) {
		current = attempt{reset: now.Add(10 * time.Minute)}
	}
	if current.count >= 8 {
		l.attempts[key] = current
		return false
	}
	current.count++
	l.attempts[key] = current
	return true
}

func (l *loginLimiter) Success(key string) {
	l.mu.Lock()
	delete(l.attempts, key)
	l.mu.Unlock()
}
