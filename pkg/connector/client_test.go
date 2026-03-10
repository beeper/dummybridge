package connector

import (
	"testing"
	"time"

	"maunium.net/go/mautrix/event"
)

func TestGetRemoteEchoBehavior(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		pending bool
		delay   time.Duration
	}{
		{name: "normal message", body: "hello", pending: false},
		{name: "no echo trigger", body: "remote-echo none", pending: true},
		{name: "delay trigger", body: "remote-echo delay 5s", pending: true, delay: 5 * time.Second},
		{name: "case insensitive", body: "REMOTE-ECHO DELAY 2m", pending: true, delay: 2 * time.Minute},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := getRemoteEchoBehavior(&event.MessageEventContent{Body: tc.body})
			if got.pending != tc.pending {
				t.Fatalf("pending = %v, want %v", got.pending, tc.pending)
			}
			if got.delay != tc.delay {
				t.Fatalf("delay = %s, want %s", got.delay, tc.delay)
			}
		})
	}
}
