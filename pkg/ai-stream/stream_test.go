package aistream

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/beeper/dummybridge/pkg/ag-ui"
)

func TestPackRunSplitsOver64KBAndReconstructs(t *testing.T) {
	run := NewRun("run-1", "thread-1", DefaultModel, "ai", "AI", time.Unix(10, 0))
	writer := NewWriter(run, func() time.Time { return time.Unix(10, 0) })
	writer.Start()
	writer.Text(strings.Repeat("a", 70*1024))
	writer.Finish(agui.FinishReasonStop)

	carriers, err := PackRun(*run, "$anchor", CarrierBudgetBytes)
	if err != nil {
		t.Fatal(err)
	}
	if len(carriers) < 2 {
		t.Fatalf("expected multiple carriers for over-64KB output, got %d", len(carriers))
	}
	for i, carrier := range carriers {
		if size := JSONSize(CarrierContent(carrier.Envelopes)); size > CarrierBudgetBytes {
			t.Fatalf("carrier %d is %d bytes, budget %d", i, size, CarrierBudgetBytes)
		}
	}
	if got := ReconstructText(carriers); got != strings.Repeat("a", 70*1024) {
		t.Fatalf("reconstructed text length = %d", len(got))
	}
}

func TestPackRunDoesNotPutFinalizationTotalsOnStreamEnvelopes(t *testing.T) {
	run := NewRun("run-1", "thread-1", DefaultModel, "ai", "AI", time.Unix(10, 0))
	writer := NewWriter(run, func() time.Time { return time.Unix(10, 0) })
	writer.Start()
	writer.Text("hello")
	writer.Finish(agui.FinishReasonStop)

	carriers, err := PackRun(*run, "$anchor", CarrierBudgetBytes)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(CarrierContent(carriers[0].Envelopes))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "seqTotal") {
		t.Fatalf("stream envelopes must not contain finalization totals: %s", raw)
	}
}

func TestFinalSnapshotSplitsIntoBaseAndContinuationParts(t *testing.T) {
	run := NewRun("run-1", "thread-1", DefaultModel, "ai", "AI", time.Unix(10, 0))
	writer := NewWriter(run, func() time.Time { return time.Unix(10, 0) })
	writer.Start()
	writer.Thinking(strings.Repeat("t", 12*1024))
	writer.Text(strings.Repeat("a", 70*1024))
	writer.ToolStart("tool-1", "shell", 0, nil)
	writer.ToolArgs("tool-1", `{"cmd":"pwd"}`, `{"cmd":"pwd"}`)
	writer.ToolEnd("tool-1", "shell", `{"cmd":"pwd"}`, map[string]any{"ok": true})
	writer.Finish(agui.FinishReasonStop)

	carriers, err := PackRun(*run, "$anchor", CarrierBudgetBytes)
	if err != nil {
		t.Fatal(err)
	}
	var baseSnapshots, continuations int
	var reconstructedText strings.Builder
	var sawMetadata bool
	for i, carrier := range carriers {
		if size := JSONSize(CarrierContent(carrier.Envelopes)); size > CarrierBudgetBytes {
			t.Fatalf("carrier %d is %d bytes, budget %d", i, size, CarrierBudgetBytes)
		}
		for _, env := range carrier.Envelopes {
			switch env.Part["type"] {
			case agui.EventMessagesSnapshot:
				baseSnapshots++
				messages, ok := env.Part["messages"].([]any)
				if !ok || len(messages) != 1 {
					t.Fatalf("bad final base snapshot: %#v", env.Part["messages"])
				}
				message, ok := messages[0].(map[string]any)
				if !ok {
					t.Fatalf("bad final base snapshot message: %#v", messages[0])
				}
				metadata, ok := message["metadata"].(map[string]any)
				if ok && metadata["runId"] == "run-1" {
					sawMetadata = true
				}
			case agui.EventCustom:
				if env.Part["name"] != FinalPartsCustomName {
					continue
				}
				continuations++
				value := env.Part["value"].(map[string]any)
				if value["messageId"] != run.MessageID || value["runId"] != run.RunID {
					t.Fatalf("bad continuation relation data: %#v", value)
				}
				if _, ok := value["metadata"]; ok {
					t.Fatalf("continuation must not duplicate message metadata: %#v", value)
				}
				for _, part := range testFinalParts(t, value["parts"]) {
					if part["type"] == "text" {
						reconstructedText.WriteString(part["content"].(string))
					}
				}
			}
		}
	}
	if baseSnapshots != 1 || continuations == 0 || !sawMetadata {
		t.Fatalf("expected one metadata base snapshot and continuations, base=%d continuations=%d metadata=%v", baseSnapshots, continuations, sawMetadata)
	}
	if !strings.Contains(run.Text(), reconstructedText.String()) {
		t.Fatalf("unexpected continuation text reconstruction length=%d", reconstructedText.Len())
	}
}

func testFinalParts(t *testing.T, value any) []map[string]any {
	t.Helper()
	switch parts := value.(type) {
	case []agui.MessagePart:
		out := make([]map[string]any, 0, len(parts))
		for _, part := range parts {
			out = append(out, map[string]any(part))
		}
		return out
	case []any:
		out := make([]map[string]any, 0, len(parts))
		for _, rawPart := range parts {
			part, ok := rawPart.(map[string]any)
			if !ok {
				t.Fatalf("bad final part: %#v", rawPart)
			}
			out = append(out, part)
		}
		return out
	default:
		t.Fatalf("bad final parts: %#v", value)
		return nil
	}
}

func TestPackRunUsesDeltaEventsInsteadOfAccumulatedText(t *testing.T) {
	run := NewRun("run-1", "thread-1", DefaultModel, "ai", "AI", time.Unix(10, 0))
	tick := int64(10)
	writer := NewWriter(run, func() time.Time {
		tick++
		return time.Unix(tick, 0)
	})
	writer.Start()
	writer.Text("abc")
	writer.Text("def")
	writer.Finish(agui.FinishReasonStop)

	carriers, err := PackRun(*run, "$anchor", CarrierBudgetBytes)
	if err != nil {
		t.Fatal(err)
	}
	if len(carriers) != 1 {
		t.Fatalf("under-budget run should be packed into one carrier, got %d", len(carriers))
	}
	var deltas []string
	for _, carrier := range carriers {
		for _, env := range carrier.Envelopes {
			if env.Part["type"] == agui.EventTextMessageContent {
				deltas = append(deltas, env.Part["delta"].(string))
			}
		}
	}
	if strings.Join(deltas, "|") != "abc|def" {
		t.Fatalf("expected original deltas only, got %#v", deltas)
	}
}

func TestRawEventIsTruncatedBeforePacking(t *testing.T) {
	run := NewRun("run-1", "thread-1", DefaultModel, "ai", "AI", time.Unix(10, 0))
	builder := agui.NewEventBuilder(DefaultModel, func() time.Time { return time.Unix(10, 0) })
	run.Events = append(run.Events, builder.Custom("com.beeper.debug", map[string]any{"ok": true}))
	run.Events[0]["rawEvent"] = strings.Repeat("x", CarrierBudgetBytes)

	carriers, err := PackRun(*run, "$anchor", CarrierBudgetBytes)
	if err != nil {
		t.Fatal(err)
	}
	part := carriers[0].Envelopes[0].Part
	if part["rawEventTruncated"] != true {
		t.Fatalf("expected rawEventTruncated marker, got %#v", part)
	}
	if size := JSONSize(CarrierContent(carriers[0].Envelopes)); size > CarrierBudgetBytes {
		t.Fatalf("carrier size = %d, budget %d", size, CarrierBudgetBytes)
	}
}

func TestPackRunRejectsOversizedNonTextEvent(t *testing.T) {
	run := NewRun("run-1", "thread-1", DefaultModel, "ai", "AI", time.Unix(10, 0))
	builder := agui.NewEventBuilder(DefaultModel, func() time.Time { return time.Unix(10, 0) })
	run.Events = append(run.Events, builder.Custom("com.beeper.large", map[string]any{
		"value": strings.Repeat("x", CarrierBudgetBytes),
	}))

	_, err := PackRun(*run, "$anchor", CarrierBudgetBytes)
	if err == nil {
		t.Fatal("expected oversized non-text event to fail packing")
	}
}

func TestValidateRejectsLegacyOrInvalidToolResultShape(t *testing.T) {
	run := NewRun("run-1", "thread-1", DefaultModel, "ai", "AI", time.Unix(10, 0))
	builder := agui.NewEventBuilder(DefaultModel, func() time.Time { return time.Unix(10, 0) })
	run.Events = append(run.Events,
		builder.RunStarted("thread-1", "run-1"),
		builder.ToolCallStart("msg-run-1", "tool-1", "shell", nil, nil),
		builder.ToolCallEnd("tool-1", "shell", nil, map[string]any{"ok": true}, agui.ToolStateInputComplete),
	)
	if err := run.Validate(); err == nil {
		t.Fatal("expected validation error for non-string TOOL_CALL_END.result")
	}
}

func TestApprovalResolverMatchesEmojiKeysAndAliases(t *testing.T) {
	options := DefaultApprovalOptions("approval-1")
	for _, key := range []string{"👍", "approval.allow_once", "allow"} {
		option, ok := ResolveReaction(options, key)
		if !ok || !option.Value.Approved || option.Value.Always {
			t.Fatalf("expected allow-once for %q, got %#v ok=%v", key, option, ok)
		}
	}
	option, ok := ResolveReaction(options, "always")
	if !ok || !option.Value.Approved || !option.Value.Always {
		t.Fatalf("expected allow-always, got %#v ok=%v", option, ok)
	}
	option, ok = ResolveReaction(options, "👎")
	if !ok || option.Value.Approved || option.Value.Reason != "denied" {
		t.Fatalf("expected denial, got %#v ok=%v", option, ok)
	}
}

func TestCleanupKeepsSelectedUserReactionAndRemovesBridgeOptions(t *testing.T) {
	options := DefaultApprovalOptions("approval-1")
	cleanup := CleanupReactions(options, "👍", []ReactionEvent{
		{EventID: "$bridge-allow", Sender: "ai", Key: "👍", Bridge: true},
		{EventID: "$bridge-deny", Sender: "ai", Key: "👎", Bridge: true},
		{EventID: "$user-allow", Sender: "@user:example", Key: "👍"},
		{EventID: "$user-deny", Sender: "@user:example", Key: "👎"},
	}, "ai")
	if !cleanup.Matched || cleanup.SelectedReactionEvent != "$user-allow" {
		t.Fatalf("bad selected reaction: %#v", cleanup)
	}
	got := strings.Join(cleanup.RedactReactionEvents, ",")
	if !strings.Contains(got, "$bridge-allow") || !strings.Contains(got, "$bridge-deny") || !strings.Contains(got, "$user-deny") {
		t.Fatalf("bad cleanup redactions: %#v", cleanup.RedactReactionEvents)
	}
}

func TestApprovalResponseRunEmitsRespondedStateAndToolResult(t *testing.T) {
	run := ApprovalResponseRun(ApprovalContext{
		ID:          "approval-1",
		ThreadID:    "thread-1",
		RunID:       "run-1",
		MessageID:   "msg-1",
		ToolCallID:  "tool-1",
		ToolName:    "shell",
		TargetEvent: "$anchor",
		SeqStart:    10,
		PreviewText: "Use supportbrief for incremental patches.",
	}, agui.ToolApprovalResponse{
		Approved: false,
		Reason:   "denied",
		Fields:   map[string]any{"scope": "once"},
		Metadata: map[string]any{"source": "reaction"},
	}, time.Unix(10, 0))

	if run.RunID != "run-1" || run.MessageID != "msg-1" {
		t.Fatalf("approval response must continue the existing run/message, got %#v", run)
	}
	if run.Preview.Text != "Use supportbrief for incremental patches." {
		t.Fatalf("approval response must preserve anchor preview, got %#v", run.Preview)
	}
	if len(run.Events) != 2 {
		t.Fatalf("expected approval response and tool result events, got %#v", run.Events)
	}
	if run.Events[0]["type"] != agui.EventCustom || run.Events[0]["name"] != agui.ApprovalCustomResponded {
		t.Fatalf("missing approval-responded event: %#v", run.Events[0])
	}
	if run.Events[1]["type"] != agui.EventToolCallEnd || run.Events[1]["state"] != agui.ToolStateApprovalResponded {
		t.Fatalf("missing approval-responded tool end: %#v", run.Events[1])
	}
	result := jsonMap(t, run.Events[1]["result"])
	if result["state"] != agui.ToolResultStateError || result["reason"] != "denied" {
		t.Fatalf("expected structured denied result, got %#v", result)
	}
	if result["fields"].(map[string]any)["scope"] != "once" || result["metadata"].(map[string]any)["source"] != "reaction" {
		t.Fatalf("expected flexible approval fields to survive, got %#v", result)
	}
	if run.Approvals[0].Fields["scope"] != "once" || run.Approvals[0].Metadata["source"] != "reaction" {
		t.Fatalf("expected approval summary fields to survive, got %#v", run.Approvals[0])
	}

	carriers, err := PackRunFromSeq(run, "$anchor", CarrierBudgetBytes, 10)
	if err != nil {
		t.Fatal(err)
	}
	if carriers[0].Envelopes[0].Seq != 10 {
		t.Fatalf("expected continuation seq 10, got %#v", carriers[0].Envelopes[0])
	}
}

func TestApprovalResponseRunPreservesApprovedAlways(t *testing.T) {
	run := ApprovalResponseRun(ApprovalContext{
		ID:          "approval-1",
		ThreadID:    "thread-1",
		RunID:       "run-1",
		MessageID:   "msg-1",
		ToolCallID:  "tool-1",
		ToolName:    "shell",
		TargetEvent: "$anchor",
	}, agui.ToolApprovalResponse{Approved: true, Always: true}, time.Unix(10, 0))

	if run.Status.State != "complete" {
		t.Fatalf("expected complete approval response run, got %#v", run.Status)
	}
	if len(run.Approvals) != 1 || run.Approvals[0].State != "approved-always" || !run.Approvals[0].Always {
		t.Fatalf("bad approval summary: %#v", run.Approvals)
	}
	result := jsonMap(t, run.Events[1]["result"])
	if result["state"] != agui.ToolResultStateComplete || result["approved"] != true || result["always"] != true {
		t.Fatalf("bad approval result: %#v", result)
	}
}

func jsonMap(t *testing.T, value any) map[string]any {
	t.Helper()
	text, ok := value.(string)
	if !ok {
		t.Fatalf("expected JSON string result, got %#v", value)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("failed to parse result %q: %v", text, err)
	}
	return out
}
