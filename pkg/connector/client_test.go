package connector

import (
	"testing"
	"time"

	"github.com/beeper/ai-bridge/pkg/ag-ui"
	"github.com/beeper/ai-bridge/pkg/ai-stream"
	"maunium.net/go/mautrix/event"
)

func TestGetRemoteEchoBehavior(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		pending bool
		delay   time.Duration
		fail    bool
	}{
		{name: "normal message", body: "hello", pending: false},
		{name: "no echo trigger", body: "remote-echo none", pending: true},
		{name: "fail trigger", body: "remote-echo fail", fail: true},
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
			if got.fail != tc.fail {
				t.Fatalf("fail = %v, want %v", got.fail, tc.fail)
			}
		})
	}
}

func TestSleepUntilCarrierTimeWithoutConnectedContext(t *testing.T) {
	base := time.Now()
	run := aistream.Run{
		Events: []agui.Event{{
			"type":      agui.EventRunStarted,
			"timestamp": base.UnixMilli(),
			"threadId":  "thread-1",
		}},
	}
	carrier := aistream.Carrier{
		Envelopes: []aistream.Envelope{{
			Part: agui.Event{
				"type":      agui.EventTextMessageContent,
				"timestamp": base.Add(time.Millisecond).UnixMilli(),
				"messageId": "message-1",
			},
		}},
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		(&DummyClient{}).sleepUntilCarrierTime(run, carrier, base)
	}()

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for carrier sleep without connected context")
	}
}
