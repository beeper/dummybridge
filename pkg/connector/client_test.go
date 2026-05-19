package connector

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/beeper/dummybridge/pkg/ag-ui"
	"github.com/beeper/dummybridge/pkg/ai-stream"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
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

func TestResolveApprovalOnceKeepsFirstSelection(t *testing.T) {
	client := &DummyClient{}
	selected, first := client.resolveApprovalOnce("approval-1", "allow")
	if !first || selected != "allow" {
		t.Fatalf("first selection = %q first=%v", selected, first)
	}
	selected, first = client.resolveApprovalOnce("approval-1", "deny")
	if first || selected != "allow" {
		t.Fatalf("second selection = %q first=%v", selected, first)
	}
}

func TestInitialAIAnchorRunKeepsPreviewButNotTerminalMetadata(t *testing.T) {
	run := aistream.NewRun("run-1", "thread-1", aistream.DefaultModel, "ai", "AI", time.Unix(10, 0))
	writer := aistream.NewWriter(run, func() time.Time { return time.Unix(10, 0) })
	writer.Start()
	writer.Text("visible preview")
	writer.Finish(agui.FinishReasonStop)

	anchor := initialAIAnchorRun(*run)
	if anchor.Preview.Text == "" {
		t.Fatal("expected anchor to keep useful preview text")
	}
	if anchor.Status.State != "streaming" {
		t.Fatalf("anchor status = %#v, want streaming", anchor.Status)
	}
	if anchor.Usage.TotalTokens != 0 || anchor.Usage.CompletionTokens != 0 || anchor.Usage.PromptTokens != 0 {
		t.Fatalf("anchor leaked terminal usage: %#v", anchor.Usage)
	}
	if run.Status.State != "complete" || run.Usage.TotalTokens == 0 {
		t.Fatalf("final run should keep terminal metadata: status=%#v usage=%#v", run.Status, run.Usage)
	}
}

func TestApprovalContextForMessageFallsBackToStoredMessage(t *testing.T) {
	want := aistream.ApprovalContext{
		ID:          "approval-1",
		ThreadID:    "thread-1",
		RunID:       "run-1",
		MessageID:   "msg-1",
		ToolCallID:  "tool-1",
		TargetEvent: "$event",
		SeqStart:    12,
	}
	stub := &database.Message{ID: "approval-1"}
	rawMetadata, err := json.Marshal(map[string]any{"com.beeper.ai.approval": want})
	if err != nil {
		t.Fatal(err)
	}
	fetched := &database.Message{ID: "approval-1", Metadata: rawMetadata}
	called := false

	got, ok := approvalContextForMessage(context.Background(), stub, func(_ context.Context, messageID networkid.MessageID) (*database.Message, error) {
		called = true
		if messageID != stub.ID {
			t.Fatalf("fetch message ID = %q, want %q", messageID, stub.ID)
		}
		return fetched, nil
	})
	if !ok {
		t.Fatal("expected approval context")
	}
	if !called {
		t.Fatal("expected fallback fetch")
	}
	if got.ID != want.ID || got.RunID != want.RunID || got.TargetEvent != want.TargetEvent || got.SeqStart != want.SeqStart {
		t.Fatalf("approval context = %#v, want %#v", got, want)
	}
}
