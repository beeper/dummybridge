package aibridgev2

import (
	"strings"
	"testing"
	"time"

	aistream "github.com/beeper/dummybridge/pkg/ai-stream"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

func TestBridgeV2AIEvents(t *testing.T) {
	now := time.Unix(10, 0)
	run := aistream.NewRun("run-1", "thread-1", "", "ai", "AI", now)
	run.Preview = aistream.Preview{Text: "visible preview"}

	anchor := Anchor(
		networkid.PortalKey{ID: "portal-1"},
		networkid.UserID("ai"),
		*run,
		now,
	)
	if anchor.Type != bridgev2.RemoteEventMessage {
		t.Fatalf("anchor type = %v", anchor.Type)
	}
	part := anchor.Data.Parts[0]
	if part.Type != event.EventMessage || part.Content.Body != "visible preview" {
		t.Fatalf("unexpected anchor part: %#v", part)
	}
	if part.Extra[aistream.BeeperAIKey] == nil || part.Extra[aistream.BeeperAIMetadataKey] == nil {
		t.Fatalf("anchor missing AI metadata: %#v", part.Extra)
	}
	stream, ok := part.Extra["com.beeper.stream"].(map[string]any)
	if !ok || stream["type"] != aistream.BeeperAIStreamDeltas {
		t.Fatalf("anchor missing stream descriptor: %#v", part.Extra)
	}

	carrier := Carrier(
		networkid.PortalKey{ID: "portal-1"},
		networkid.UserID("ai"),
		*run,
		aistream.Carrier{Envelopes: []aistream.Envelope{{
			Seq:         1,
			RunID:       run.RunID,
			ThreadID:    run.ThreadID,
			TargetEvent: "$anchor",
		}}},
		id.EventID("$anchor"),
		1,
		now,
	)
	carrierPart := carrier.Data.Parts[0]
	if carrierPart.Content.MsgType != event.MsgText || carrierPart.Content.Body != "" {
		t.Fatalf("carrier should be hidden text carrier: %#v", carrierPart.Content)
	}
	if carrierPart.Extra[aistream.BeeperAIStreamDeltas] == nil {
		t.Fatalf("carrier missing deltas: %#v", carrierPart.Extra)
	}

	approval := ApprovalPrompt(networkid.PortalKey{ID: "portal-1"}, networkid.UserID("ai"), aistream.ApprovalContext{
		ID:          "approval-1",
		ThreadID:    run.ThreadID,
		RunID:       run.RunID,
		MessageID:   run.MessageID,
		ToolCallID:  "tool-1",
		ToolName:    "dummy_echo",
		TargetEvent: "$anchor",
	}, now)
	approvalPart := approval.Data.Parts[0]
	approvalMetadata, ok := approvalPart.DBMetadata.(map[string]any)
	if !ok || approvalMetadata["com.beeper.ai.approval"] == nil {
		t.Fatalf("approval missing DB metadata: %#v", approvalPart.DBMetadata)
	}
}

func TestFinalMetadataEditUsesCompactAnchorContent(t *testing.T) {
	now := time.Unix(10, 0)
	run := aistream.NewRun("run-1", "thread-1", "", "ai", "AI", now)
	run.Preview = aistream.Preview{Text: strings.Repeat("a", aistream.PreviewBudgetBytes+1), Truncated: true}

	edit := FinalMetadataEdit(
		networkid.PortalKey{ID: "portal-1"},
		networkid.UserID("ai"),
		networkid.MessageID(run.MessageID),
		*run,
		now,
	)
	if edit.Type != bridgev2.RemoteEventEdit {
		t.Fatalf("final metadata event type = %v", edit.Type)
	}
	if edit.TargetMessage != networkid.MessageID(run.MessageID) {
		t.Fatalf("final metadata target = %q", edit.TargetMessage)
	}
	if edit.Data.Text() != "" {
		t.Fatalf("final metadata edit must not expose full accumulated text")
	}
}
