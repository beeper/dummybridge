package matrix

import (
	"strings"
	"testing"
	"time"

	"github.com/beeper/dummybridge/pkg/ag-ui"
	"github.com/beeper/dummybridge/pkg/ai-stream"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

func TestAnchorContentUsesVisibleTextAndAIProfile(t *testing.T) {
	run := aistream.NewRun("run-1", "thread-1", aistream.DefaultModel, "ai", "AI", time.Unix(10, 0))
	run.Preview = aistream.Preview{Text: "visible preview"}

	content, extra := AnchorContent(*run)
	if content.MsgType != event.MsgText || content.Body != "visible preview" {
		t.Fatalf("bad anchor content: %#v", content)
	}
	if content.Format != event.FormatHTML || content.FormattedBody == "" {
		t.Fatalf("anchor preview should include Matrix HTML: %#v", content)
	}
	if content.BeeperPerMessageProfile == nil || content.BeeperPerMessageProfile.ID != "ai" || content.BeeperPerMessageProfile.Displayname != "AI" {
		t.Fatalf("missing AI per-message profile: %#v", content.BeeperPerMessageProfile)
	}
	uiMessage, ok := extra[aistream.BeeperAIKey].(map[string]any)
	if !ok || uiMessage["id"] == "" || uiMessage["metadata"] != nil {
		t.Fatalf("bad compact AI message: %#v", extra[aistream.BeeperAIKey])
	}
	if extra[aistream.BeeperAIMetadataKey] == nil {
		t.Fatalf("missing AI metadata: %#v", extra)
	}
	stream, ok := extra["com.beeper.stream"].(map[string]any)
	if !ok || stream["user_id"] != nil || stream["type"] != aistream.BeeperAIStreamDeltas {
		t.Fatalf("missing stream descriptor: %#v", extra["com.beeper.stream"])
	}
}

func TestAnchorContentKeepsLongRunsCompact(t *testing.T) {
	run := aistream.NewRun("run-1", "thread-1", aistream.DefaultModel, "ai", "AI", time.Unix(10, 0))
	writer := aistream.NewWriter(run, func() time.Time { return time.Unix(10, 0) })
	writer.Start()
	writer.Text(strings.Repeat("a", 70*1024))
	writer.Finish(agui.FinishReasonStop)

	content, extra := AnchorContent(*run)
	if len(content.Body) > aistream.PreviewBudgetBytes {
		t.Fatalf("anchor body length = %d, want <= %d", len(content.Body), aistream.PreviewBudgetBytes)
	}
	metadata := extra[aistream.BeeperAIMetadataKey].(map[string]any)
	if _, hasParts := metadata["parts"]; hasParts {
		t.Fatalf("metadata must not contain streamed parts: %#v", metadata)
	}
	if _, hasChunks := metadata["chunks"]; hasChunks {
		t.Fatalf("metadata must not contain streamed chunks: %#v", metadata)
	}
	preview := metadata["preview"].(aistream.Preview)
	if !preview.Truncated || len(preview.Text) > aistream.PreviewBudgetBytes {
		t.Fatalf("bad bounded preview: %#v", preview)
	}
}

func TestAnchorContentRendersFinalPreviewAsMatrixHTML(t *testing.T) {
	run := aistream.NewRun("run-1", "thread-1", aistream.DefaultModel, "ai", "AI", time.Unix(10, 0))
	run.Preview = aistream.Preview{Text: "Use **bold** and `code`"}

	content, _ := AnchorContent(*run)
	if content.Format != event.FormatHTML {
		t.Fatalf("format = %q, want Matrix HTML", content.Format)
	}
	if !strings.Contains(content.FormattedBody, "<strong>bold</strong>") || !strings.Contains(content.FormattedBody, "<code>code</code>") {
		t.Fatalf("formatted body did not render markdown: %q", content.FormattedBody)
	}
}

func TestCarrierContentIsHiddenTextCarrierWithDeltas(t *testing.T) {
	carrier := aistream.Carrier{Envelopes: []aistream.Envelope{{
		ThreadID:    "thread-1",
		RunID:       "run-1",
		MessageID:   "msg-run-1",
		Seq:         1,
		TargetEvent: "$anchor",
	}}}

	content, extra := CarrierContent(carrier, id.EventID("$anchor"))
	if content.MsgType != event.MsgText || content.Body != "" {
		t.Fatalf("carrier should be empty m.text, got %#v", content)
	}
	if content.RelatesTo == nil || content.RelatesTo.EventID != "$anchor" {
		t.Fatalf("carrier should reference anchor, got %#v", content.RelatesTo)
	}
	deltas, ok := extra[aistream.BeeperAIStreamDeltas].([]aistream.Envelope)
	if !ok || len(deltas) != 1 || deltas[0].Seq != 1 {
		t.Fatalf("missing deltas: %#v", extra)
	}
}

func TestApprovalContentIncludesContextAndGenericReactionOptions(t *testing.T) {
	ctx := aistream.ApprovalContext{
		ID:          "approval-1",
		ThreadID:    "thread-1",
		RunID:       "run-1",
		MessageID:   "msg-run-1",
		ToolCallID:  "tool-1",
		ToolName:    "shell",
		TargetEvent: "$anchor",
	}
	options := aistream.DefaultApprovalOptions(ctx.ID)

	content, extra := ApprovalContent(ctx, options)
	if content.MsgType != event.MsgText || content.RelatesTo == nil || content.RelatesTo.EventID != "$anchor" {
		t.Fatalf("bad approval content: %#v", content)
	}
	meta, ok := extra["com.beeper.ai.approval"].(map[string]any)
	if !ok {
		t.Fatalf("missing approval metadata: %#v", extra)
	}
	if meta["id"] != ctx.ID || meta["runId"] != ctx.RunID || meta["messageId"] != ctx.MessageID || meta["toolCallId"] != ctx.ToolCallID {
		t.Fatalf("bad approval metadata: %#v", meta)
	}
	reactions, ok := meta["reactions"].([]any)
	if !ok || len(reactions) != len(options) {
		t.Fatalf("bad approval reactions: %#v", meta["reactions"])
	}
	first := reactions[0].(map[string]any)
	if first["id"] != aistream.ApprovalReactionAllowOnce {
		t.Fatalf("bad first reaction option: %#v", first)
	}
}
