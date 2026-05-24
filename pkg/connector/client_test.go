package connector

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/beeper/ai-bridge/pkg/ag-ui"
	"github.com/beeper/ai-bridge/pkg/ai-stream"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
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

func TestAIDemoCommandContentOnlyMatchesExplicitDemoCommands(t *testing.T) {
	for _, body := range []string{
		"help",
		"/help",
		"!help",
		"dummybridge help",
		"stream 20",
		"stream-tools 100 shell",
		"stream 1 --runs=2",
	} {
		if !isAIDemoCommandContent(&event.MessageEventContent{Body: body}) {
			t.Fatalf("expected AI demo command for %q", body)
		}
	}
	for _, body := range []string{
		"",
		"hello",
		"dummybridge",
		"remote-echo delay 1s",
	} {
		if isAIDemoCommandContent(&event.MessageEventContent{Body: body}) {
			t.Fatalf("did not expect AI demo command for %q", body)
		}
	}
}

func TestDummyAISenderForPortalSupportsDedicatedAndNormalRooms(t *testing.T) {
	if got := dummyAISenderForPortal(&bridgev2.Portal{Portal: &database.Portal{PortalKey: networkid.PortalKey{ID: "ai-room"}}}); got != aiGhostID {
		t.Fatalf("AI portal sender = %q, want %q", got, aiGhostID)
	}
	if got := dummyAISenderForPortal(&bridgev2.Portal{Portal: &database.Portal{PortalKey: networkid.PortalKey{ID: "normal-room"}}}); got != stablePortalUserIDByIndex("normal-room", 0) {
		t.Fatalf("normal portal sender = %q", got)
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

func TestInitialAIAnchorRunOmitsPreviewAndTerminalMetadata(t *testing.T) {
	run := aistream.NewRun("run-1", "thread-1", aistream.DefaultModel, "ai", "AI", time.Unix(10, 0))
	writer := aistream.NewWriter(run, func() time.Time { return time.Unix(10, 0) })
	writer.Start()
	writer.Text("visible preview")
	writer.Finish(agui.FinishReasonStop)

	anchor := initialAIAnchorRun(*run)
	if anchor.Preview.Text != "" {
		t.Fatalf("anchor should not include initial preview text: %#v", anchor.Preview)
	}
	uiMessage := anchor.InitialUIMessage()
	if len(uiMessage.Parts) != 0 {
		t.Fatalf("anchor UI message should wait for stream deltas: %#v", uiMessage.Parts)
	}
	if uiMessage.Metadata["runId"] != run.RunID {
		t.Fatalf("anchor UI metadata missing run id: %#v", uiMessage.Metadata)
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

func TestCarrierTimestampUsesEventOffsetFromRunStart(t *testing.T) {
	run := aistream.Run{
		Events: []agui.Event{
			{"timestamp": int64(10_000), "type": agui.EventRunStarted, "threadId": "thread-1"},
			{"timestamp": int64(13_500), "type": agui.EventTextMessageContent, "messageId": "msg-1", "delta": "later"},
		},
	}
	streamStart := time.Unix(100, 0)
	target := carrierTimestamp(run, aistream.Carrier{Envelopes: []aistream.Envelope{{
		Part: run.Events[1],
	}}}, streamStart)
	if want := streamStart.Add(3500 * time.Millisecond); !target.Equal(want) {
		t.Fatalf("target = %s, want %s", target, want)
	}
}

func TestSplitCarriersForTimedEmissionKeepsOneEnvelopePerCarrier(t *testing.T) {
	carriers := splitCarriersForTimedEmission([]aistream.Carrier{{
		Envelopes: []aistream.Envelope{
			{Seq: 1},
			{Seq: 2},
		},
	}})
	if len(carriers) != 2 {
		t.Fatalf("carrier count = %d, want 2", len(carriers))
	}
	if carriers[0].Envelopes[0].Seq != 1 || carriers[1].Envelopes[0].Seq != 2 {
		t.Fatalf("bad split carriers: %#v", carriers)
	}
}

func TestApprovalContextForMessageFallsBackToStoredMessage(t *testing.T) {
	want := aistream.ApprovalContext{
		ID:          "approval-1",
		ThreadID:    "thread-1",
		RunID:       "run-1",
		MessageID:   "msg-1",
		Command:     "stream-tools 120 shell#approval",
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

func TestApprovalOptionReactionIsBridgeManagedFallback(t *testing.T) {
	msg := &bridgev2.MatrixReaction{
		MatrixEventBase: bridgev2.MatrixEventBase[*event.ReactionEventContent]{
			Event: &event.Event{Content: event.Content{Raw: map[string]any{
				"com.beeper.ai.approval_option": map[string]any{"choice": "approve"},
			}}},
		},
	}
	if !isApprovalOptionReaction(msg) {
		t.Fatal("expected managed approval option reaction")
	}
	if isApprovalOptionReaction(&bridgev2.MatrixReaction{MatrixEventBase: bridgev2.MatrixEventBase[*event.ReactionEventContent]{Event: &event.Event{Content: event.Content{Raw: map[string]any{}}}}}) {
		t.Fatal("plain user reaction must not be treated as a managed approval option")
	}
}

func TestAnnotateApprovalEventIDsAddsReactionTargetEventToStreamPrompt(t *testing.T) {
	run := aistream.NewRun("run-1", "thread-1", aistream.DefaultModel, "ai", "AI", time.Unix(10, 0))
	writer := aistream.NewWriter(run, func() time.Time { return time.Unix(10, 0) })
	writer.ToolApprovalRequested("tool-1", "shell", map[string]any{"command": "ls"}, agui.ToolApproval{
		ID:            "approval-1",
		NeedsApproval: true,
	})

	annotateApprovalEventIDs(run, map[string]id.EventID{
		"approval-1": "$approval",
	})

	for _, evt := range run.Events {
		if evt["type"] != agui.EventCustom || evt["name"] != agui.ApprovalCustomRequested {
			continue
		}
		value, _ := evt["value"].(map[string]any)
		if value["approvalMessageId"] != "approval-1" || value["approvalEventId"] != "$approval" {
			t.Fatalf("approval stream event missing target ids: %#v", value)
		}
		return
	}
	t.Fatal("missing approval-requested event")
}

func TestApprovalDecisionsAreStoredInRunSession(t *testing.T) {
	client := &DummyClient{}
	first := agui.ToolApprovalResponse{ID: "approval-1", Approved: true}
	decisions := client.recordAIApprovalDecision("run-1", first)
	if len(decisions) != 1 || !decisions["approval-1"].Approved {
		t.Fatalf("bad first decisions: %#v", decisions)
	}
	second := agui.ToolApprovalResponse{ID: "approval-2", Approved: false, Reason: "denied"}
	decisions = client.recordAIApprovalDecision("run-1", second)
	if len(decisions) != 2 || !decisions["approval-1"].Approved || decisions["approval-2"].Reason != "denied" {
		t.Fatalf("bad accumulated decisions: %#v", decisions)
	}
}
