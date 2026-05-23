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
	var baseText string
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
				for _, part := range testFinalParts(t, message["parts"]) {
					if part["type"] == "text" {
						baseText += part["content"].(string)
					}
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
	if baseText == "" {
		t.Fatal("base final snapshot must keep visible text in the primary event")
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

func TestFinalUIMessageCarriesToolCallMetadata(t *testing.T) {
	run := NewRun("run-1", "thread-1", DefaultModel, "ai", "AI", time.Unix(10, 0))
	writer := NewWriter(run, func() time.Time { return time.Unix(10, 0) })
	writer.ToolStartWithMetadata("tool-1", "calendar.get_events", 0, nil, map[string]any{
		"displayName": "List Calendar Events",
		"iconUrl":     "mxc://beeper.com/calendar",
	})

	message := run.FinalUIMessage(0, true)
	if len(message.Parts) != 1 {
		t.Fatalf("expected one part, got %#v", message.Parts)
	}
	metadata, ok := message.Parts[0]["metadata"].(map[string]any)
	if !ok || metadata["displayName"] != "List Calendar Events" || metadata["iconUrl"] != "mxc://beeper.com/calendar" {
		t.Fatalf("bad tool metadata: %#v", message.Parts[0])
	}
}

func TestApprovalResolverMatchesEmojiKeysAndAliases(t *testing.T) {
	choices := DefaultApprovalChoices()
	for _, key := range []string{"✅", "approve"} {
		choice, ok := ResolveApprovalChoice(choices, key)
		response := ApprovalResponseForChoice("approval-1", choice)
		if !ok || !response.Approved || response.Always {
			t.Fatalf("expected approve for %q, got %#v ok=%v", key, choice, ok)
		}
	}
	choice, ok := ResolveApprovalChoice(choices, "☑️")
	response := ApprovalResponseForChoice("approval-1", choice)
	if !ok || !response.Approved || !response.Always {
		t.Fatalf("expected always-approve, got %#v ok=%v", choice, ok)
	}
	choice, ok = ResolveApprovalChoice(choices, "deny")
	response = ApprovalResponseForChoice("approval-1", choice)
	if !ok || response.Approved || response.Reason != "denied" {
		t.Fatalf("expected denial, got %#v ok=%v", choice, ok)
	}
}

func TestApprovalRequestedValueOwnsStreamPayloadShape(t *testing.T) {
	run := NewRun("run-1", "thread-1", DefaultModel, "ai", "AI", time.Unix(10, 0))
	run.MessageID = "msg-run-1"
	approval := agui.ToolApproval{ID: "approval-1", NeedsApproval: true}

	value := NewApprovalRequestedValue(*run, "tool-1", "shell", map[string]any{"command": "ls"}, approval).Map()

	if value["threadId"] != "thread-1" || value["runId"] != "run-1" || value["messageId"] != "msg-run-1" {
		t.Fatalf("bad run identifiers: %#v", value)
	}
	if value["toolCallId"] != "tool-1" || value["toolName"] != "shell" {
		t.Fatalf("bad tool identifiers: %#v", value)
	}
	if value["approvalMessageId"] != "approval-1" {
		t.Fatalf("missing approval message id: %#v", value)
	}
	if _, ok := value["approvalEventId"]; ok {
		t.Fatalf("approval event id should only be added after Matrix send: %#v", value)
	}
	choices, ok := value["choices"].([]ApprovalChoice)
	if !ok || len(choices) != len(DefaultApprovalChoices()) || choices[0].Key != ApprovalChoiceApprove {
		t.Fatalf("bad approval choices: %#v", value["choices"])
	}
	if ApprovalIDFromRequestedValue(value) != "approval-1" {
		t.Fatalf("approval id resolver failed for value: %#v", value)
	}
	if !SetApprovalRequestedEventID(value, "$approval") || value["approvalEventId"] != "$approval" {
		t.Fatalf("failed to annotate approval event id: %#v", value)
	}
}

func TestRunMetadataOwnsMatrixPayloadShape(t *testing.T) {
	run := NewRun("run-1", "thread-1", DefaultModel, "agent-1", "Agent", time.Unix(10, 0))
	run.MessageID = "msg-run-1"
	run.Usage = agui.Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3}
	run.Preview = Preview{Text: "hello", Truncated: false}

	metadata := run.Metadata()

	if metadata["schema"] != "com.beeper.ai.run.v1" || metadata["protocol"] != "ag-ui" {
		t.Fatalf("bad protocol metadata: %#v", metadata)
	}
	if metadata["threadId"] != "thread-1" || metadata["runId"] != "run-1" || metadata["messageId"] != "msg-run-1" {
		t.Fatalf("bad run identifiers: %#v", metadata)
	}
	agent, ok := metadata["agent"].(map[string]any)
	if !ok || agent["id"] != "agent-1" || agent["displayName"] != "Agent" {
		t.Fatalf("bad agent metadata: %#v", metadata["agent"])
	}
	usage, ok := metadata["usage"].(map[string]any)
	if !ok || usage["promptTokens"] != 1 || usage["completionTokens"] != 2 || usage["totalTokens"] != 3 {
		t.Fatalf("bad usage metadata: %#v", metadata["usage"])
	}
	if _, ok := metadata["usageDetails"].(map[string]any); !ok {
		t.Fatalf("usage details should always be present: %#v", metadata)
	}
}

func TestApprovalNoticeOwnsHiddenMessagePayloadShape(t *testing.T) {
	notice := NewApprovalNotice(ApprovalContext{
		ID:         "approval-1",
		MessageID:  "msg-run-1",
		ToolCallID: "tool-1",
		ToolName:   "shell",
	}, DefaultApprovalChoices()).Map()

	if notice["schema"] != "com.beeper.ai.approval.v1" || notice["state"] != "requested" {
		t.Fatalf("bad approval notice metadata: %#v", notice)
	}
	if notice["id"] != "approval-1" || notice["messageId"] != "msg-run-1" || notice["toolCallId"] != "tool-1" || notice["toolName"] != "shell" {
		t.Fatalf("bad approval notice identifiers: %#v", notice)
	}
	choices, ok := notice["choices"].([]any)
	if !ok || len(choices) != 3 {
		t.Fatalf("bad approval notice choices: %#v", notice["choices"])
	}
	first, ok := choices[0].(map[string]any)
	if !ok || first["key"] != ApprovalChoiceApprove || first["label"] != "Allow once" || first["alias"] != "✅" {
		t.Fatalf("bad first approval choice: %#v", choices[0])
	}
	if _, ok := first["style"]; ok {
		t.Fatalf("empty style should be omitted from approval choices: %#v", first)
	}
	deny, ok := choices[2].(map[string]any)
	if !ok || deny["style"] != "danger" {
		t.Fatalf("deny choice should keep danger style: %#v", choices[2])
	}
}

func TestCleanupKeepsSelectedUserReactionAndRemovesBridgeOptions(t *testing.T) {
	choices := DefaultApprovalChoices()
	cleanup := CleanupApprovalReactions(choices, "✅", []ReactionEvent{
		{EventID: "$bridge-allow", Sender: "ai", Key: "✅", Bridge: true},
		{EventID: "$bridge-deny", Sender: "ai", Key: "❌", Bridge: true},
		{EventID: "$user-allow", Sender: "@user:example", Key: "✅"},
		{EventID: "$user-deny", Sender: "@user:example", Key: "❌"},
	}, "ai")
	if !cleanup.Matched || cleanup.SelectedReactionEvent != "$user-allow" {
		t.Fatalf("bad selected reaction: %#v", cleanup)
	}
	got := strings.Join(cleanup.RedactReactionEvents, ",")
	if !strings.Contains(got, "$bridge-allow") || !strings.Contains(got, "$bridge-deny") || !strings.Contains(got, "$user-deny") {
		t.Fatalf("bad cleanup redactions: %#v", cleanup.RedactReactionEvents)
	}
}
