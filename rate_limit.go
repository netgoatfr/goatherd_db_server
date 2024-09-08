package main

import (
	"net/http"
	"sync"
	"time"
)

type RateLimiter struct {
	limit   int           // Max requests per client
	window  time.Duration // Time window for the rate limit
	clients map[string]*Client
	mu      sync.Mutex
}

type Client struct {
	requests int
	reset    time.Time
}

func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	// Return a DEFAULT limiter
	return &RateLimiter{
		limit:   limit,
		window:  window,
		clients: make(map[string]*Client),
	}
}
func (rl *RateLimiter) Limit(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Check for the special header
		token := r.Header.Get("Authorization")
		perms, _ := getTokenPermissions(authDB, token)

		var limit int
		if perms.Ratelimit < 0 { // If rate limit is below zero (-1) then no limit
			next.ServeHTTP(w, r) // don't even bother registering the client
			return
		} else if perms.Ratelimit == 0 {
			limit = rl.limit // Standard limit
		} else {
			limit = perms.Ratelimit
		}

		clientIP := r.RemoteAddr
		rl.mu.Lock()
		defer rl.mu.Unlock()

		// Check if the client already exist
		// and add one request.
		client, exists := rl.clients[clientIP]
		if !exists || time.Now().After(client.reset) {
			client = &Client{
				requests: 1,
				reset:    time.Now().Add(rl.window),
			}
			rl.clients[clientIP] = client
		} else {
			client.requests++
		}

		// Notify that the rate is exceeded
		if client.requests > limit {
			w.Header().Set("Retry-After", time.Until(client.reset).String())
			http.Error(w, "Too many requests", http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	}
}
