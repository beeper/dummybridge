package connector

import (
	"context"
	"encoding/json"
	"math/rand"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/beeper/dummybridge/pkg/ag-ui"
	"github.com/beeper/dummybridge/pkg/ai-stream"
	"maunium.net/go/mautrix/id"
)

func TestParseCommandRecognizesHelpAliases(t *testing.T) {
	for _, input := range []string{"help", "/help", "!help", "dummybridge help"} {
		cmd, err := parseCommand(input)
		if err != nil {
			t.Fatalf("parseCommand(%q) returned error: %v", input, err)
		}
		if cmd == nil || cmd.Name != "help" {
			t.Fatalf("expected help command for %q, got %#v", input, cmd)
		}
	}
}

func TestParseCommandRejectsConflictingToolTags(t *testing.T) {
	_, err := parseCommand("stream-tools 100 shell#fail#approval")
	if err == nil {
		t.Fatal("expected parse error for conflicting tool tags")
	}
}

func TestParseCommandRejectsInvalidProfilesAndOversizedOptions(t *testing.T) {
	tests := []string{
		"stream --profile=unknown",
		"stream --terminal=unknown",
		"stream --chars=1000000",
		"stream-tools 100 shell --chunk-chars=1:9999",
	}
	for _, input := range tests {
		if _, err := parseCommand(input); err == nil {
			t.Fatalf("expected parse error for %q", input)
		}
	}
}

func TestHelpTextMentionsCommandsOptionsAndToolTags(t *testing.T) {
	guide := helpText()
	for _, expected := range []string{
		"stream-tools",
		"stream",
		"--profile=balanced|tools|errors|artifacts",
		"--no-approval",
		"#provider",
		"#inputerror",
	} {
		if !strings.Contains(guide, expected) {
			t.Fatalf("help text missing %q:\n%s", expected, guide)
		}
	}
}

func TestBuildAIRunLoremIncludesArtifactsStateAndMetadata(t *testing.T) {
	run, err := buildAIRun(context.Background(), "run-1", "thread-1", "stream-tools 400 search --reasoning=80 --steps=2 --sources=1 --documents=1 --files=1 --meta --data=demo --data-transient=temp --seed=7 --chunk-chars=32:32", time.Unix(10, 0))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, evt := range run.Events {
		switch evt["type"] {
		case agui.EventTextMessageContent, agui.EventStepStarted, agui.EventStepFinished:
			seen[evt["type"].(string)] = true
		case agui.EventStateDelta:
			seen[evt["type"].(string)] = true
			if _, ok := evt["delta"].([]map[string]any); !ok {
				t.Fatalf("STATE_DELTA should use JSON Patch array, got %#v", evt["delta"])
			}
		case agui.EventCustom:
			name, _ := evt["name"].(string)
			seen[name] = true
			if name == "com.beeper.data" {
				value := evt["value"].(map[string]any)
				if value["name"] == "temp" {
					t.Fatal("transient data must not persist as metadata")
				}
			}
		}
	}
	for _, key := range []string{agui.EventTextMessageContent, agui.EventStepStarted, agui.EventStepFinished, agui.EventStateDelta, "com.beeper.source", "com.beeper.document", "com.beeper.file", "com.beeper.data", "com.beeper.data.transient"} {
		if !seen[key] {
			t.Fatalf("missing %s in events", key)
		}
	}
	metadata := run.Metadata()
	if metadata["model"] == "" || metadata["threadId"] != "thread-1" || metadata["runId"] != "run-1" {
		t.Fatalf("bad metadata: %#v", metadata)
	}
	data := metadata["data"].(map[string]any)
	if _, ok := data["temp"]; ok {
		t.Fatalf("transient data leaked into final metadata: %#v", data)
	}
}

func TestBuildAIRunToolsApprovalUsesAGUIApprovalAndPrompt(t *testing.T) {
	run, err := buildAIRun(context.Background(), "run-1", "thread-1", "stream-tools 120 shell#approval --seed=7 --chunk-chars=32:32", time.Unix(10, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(run.Prompts) != 1 {
		t.Fatalf("expected one approval prompt, got %#v", run.Prompts)
	}
	if run.Prompts[0].ID != "approval-run-1-dummy-tool-1-shell" {
		t.Fatalf("approval prompt ID = %q, want run-scoped ID", run.Prompts[0].ID)
	}
	foundToolStart := false
	seenApprovalStateBeforeCustom := false
	for _, evt := range run.Events {
		if evt["type"] == agui.EventToolCallStart {
			if evt["state"] != agui.ToolStateApprovalRequested {
				t.Fatalf("expected approval-requested tool state, got %#v", evt)
			}
			approval, ok := evt["approval"].(*agui.ToolApproval)
			if !ok {
				t.Fatalf("expected tool start approval metadata, got %#v", evt["approval"])
			}
			if approval.ID != "approval-run-1-dummy-tool-1-shell" || !approval.NeedsApproval {
				t.Fatalf("bad approval metadata: %#v", approval)
			}
			metadata, ok := evt["metadata"].(map[string]any)
			if !ok || metadata["displayName"] != "Run Command" {
				t.Fatalf("bad tool display metadata: %#v", evt["metadata"])
			}
			foundToolStart = true
		}
		if evt["type"] == agui.EventToolCallEnd {
			if evt["state"] == agui.ToolStateInputComplete {
				t.Fatalf("approval tool must not downgrade to input-complete: %#v", evt)
			}
			if evt["state"] == agui.ToolStateApprovalRequested {
				if evt["input"] != nil {
					t.Fatalf("approval input-complete event should omit placeholder input: %#v", evt)
				}
				seenApprovalStateBeforeCustom = true
			}
		}
		if evt["type"] == agui.EventCustom && evt["name"] == agui.ApprovalCustomRequested {
			if !seenApprovalStateBeforeCustom {
				t.Fatalf("approval custom event should be emitted after approval state update: %#v", run.Events)
			}
			value := evt["value"].(map[string]any)
			if _, hasOptions := value["options"]; hasOptions {
				t.Fatalf("AG-UI approval event must not embed Matrix reaction options: %#v", value)
			}
			if value["approvalMessageId"] != "approval-run-1-dummy-tool-1-shell" {
				t.Fatalf("approval event should name the Matrix reaction target: %#v", value)
			}
			metadata, ok := value["metadata"].(map[string]any)
			if !ok || metadata["displayName"] != "Run Command" {
				t.Fatalf("approval event should carry tool display metadata: %#v", value["metadata"])
			}
			choices, ok := value["choices"].([]aistream.ApprovalChoice)
			if !ok || len(choices) == 0 || choices[0].Key != aistream.ApprovalChoiceApprove {
				t.Fatalf("approval event should duplicate renderer choices: %#v", value["choices"])
			}
			if value["input"] != nil {
				t.Fatalf("approval event should omit placeholder tool input: %#v", value)
			}
		}
	}
	if !foundToolStart {
		t.Fatal("missing tool start event")
	}
	if run.Status.State != "streaming" {
		t.Fatalf("approval request should pause the run without terminal status, got %#v", run.Status)
	}
	for _, evt := range run.Events {
		if evt["type"] == agui.EventRunFinished {
			t.Fatalf("approval request should not finish the run before response: %#v", run.Events)
		}
	}
}

func TestToolDisplayMetadataIsOptional(t *testing.T) {
	if metadata := toolDisplayMetadata("unknown_tool"); metadata != nil {
		t.Fatalf("unknown tools should not invent display metadata: %#v", metadata)
	}

	metadata := toolDisplayMetadata("linear.list_issues")
	provider, _ := metadata["provider"].(map[string]any)
	if metadata["displayName"] != "List Issues" || provider["displayName"] != "Linear" {
		t.Fatalf("bad known tool metadata: %#v", metadata)
	}
	if _, ok := metadata["iconId"]; ok {
		t.Fatalf("metadata must not use iconId: %#v", metadata)
	}
}

func TestApprovalPromptSeqStartsAtNextPackedCarrierSeq(t *testing.T) {
	run, err := buildAIRun(context.Background(), "run-1", "thread-1", "stream-tools 120 shell#approval --seed=7 --chunk-chars=32:32", time.Unix(10, 0))
	if err != nil {
		t.Fatal(err)
	}
	carriers, err := aistream.PackRunFromSeq(*run, "$anchor", aistream.CarrierBudgetBytes, 1)
	if err != nil {
		t.Fatal(err)
	}
	nextSeq := aistream.NextSeq(splitCarriersForTimedEmission(carriers))
	if nextSeq <= 1 {
		t.Fatalf("expected initial stream to consume carrier sequence numbers, got %d", nextSeq)
	}

	prompt := run.Prompts[0]
	prompt.SeqStart = nextSeq
	approvalCtx := aistream.ApprovalContext{
		ID:          prompt.ID,
		ThreadID:    run.ThreadID,
		RunID:       run.RunID,
		MessageID:   run.MessageID,
		Command:     "stream-tools 120 shell#approval --seed=7 --chunk-chars=32:32",
		ToolCallID:  prompt.ToolCallID,
		ToolName:    prompt.ToolName,
		TargetEvent: "$anchor",
		AgentID:     run.AgentID,
		AgentName:   run.AgentName,
		SeqStart:    prompt.SeqStart,
	}
	continuation, err := buildAIApprovalContinuationRun(context.Background(), approvalCtx, agui.ToolApprovalResponse{
		ID:       prompt.ID,
		Approved: true,
	}, time.Unix(20, 0))
	if err != nil {
		t.Fatal(err)
	}
	continuationCarriers, err := aistream.PackRunFromSeq(continuation, "$anchor", aistream.CarrierBudgetBytes, approvalCtx.SeqStart)
	if err != nil {
		t.Fatal(err)
	}
	if len(continuationCarriers) == 0 || len(continuationCarriers[0].Envelopes) == 0 || continuationCarriers[0].Envelopes[0].Seq != nextSeq {
		t.Fatalf("continuation should start at next carrier seq %d, got %#v", nextSeq, continuationCarriers)
	}
	if continuationCarriers[0].Envelopes[0].Seq >= 100000 {
		t.Fatalf("continuation sequence has legacy large gap: %#v", continuationCarriers[0])
	}
}

func TestApprovalLifecycleCarriesNoticeTargetAndContinuation(t *testing.T) {
	command := "stream-tools 120 shell#approval fetch --seed=7 --chunk-chars=32:32"
	run, err := buildAIRun(context.Background(), "run-1", "thread-1", command, time.Unix(10, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(run.Prompts) != 1 {
		t.Fatalf("expected one approval prompt, got %#v", run.Prompts)
	}
	sizingRun := *run
	annotateApprovalEventIDs(&sizingRun, approvalEventIDPlaceholders(sizingRun.Prompts))
	initialCarriers, err := aistream.PackRunFromSeq(sizingRun, "$anchor", aistream.CarrierBudgetBytes, 1)
	if err != nil {
		t.Fatal(err)
	}
	initialCarriers = splitCarriersForTimedEmission(initialCarriers)
	nextSeq := aistream.NextSeq(initialCarriers)
	if nextSeq <= 1 {
		t.Fatalf("expected initial carriers to advance sequence, got %d", nextSeq)
	}

	prompt := run.Prompts[0]
	prompt.SeqStart = nextSeq
	approvalCtx := aistream.ApprovalContext{
		ID:          prompt.ID,
		ThreadID:    run.ThreadID,
		RunID:       run.RunID,
		MessageID:   run.MessageID,
		Command:     command,
		ToolCallID:  prompt.ToolCallID,
		ToolName:    prompt.ToolName,
		TargetEvent: "$anchor",
		AgentID:     run.AgentID,
		AgentName:   run.AgentName,
		SeqStart:    prompt.SeqStart,
	}
	notice := aistream.NewApprovalNotice(approvalCtx, aistream.DefaultApprovalChoices()).Map()
	if notice["id"] != prompt.ID || notice["messageId"] != run.MessageID || notice["state"] != "requested" {
		t.Fatalf("approval notice does not target the paused run: %#v", notice)
	}

	annotateApprovalEventIDs(run, map[string]id.EventID{prompt.ID: "$approval"})
	annotatedCarriers, err := aistream.PackRunFromSeq(*run, "$anchor", aistream.CarrierBudgetBytes, 1)
	if err != nil {
		t.Fatal(err)
	}
	var annotatedValue map[string]any
	for _, carrier := range annotatedCarriers {
		for _, env := range carrier.Envelopes {
			if env.Part["type"] != agui.EventCustom || env.Part["name"] != agui.ApprovalCustomRequested {
				continue
			}
			annotatedValue, _ = env.Part["value"].(map[string]any)
		}
	}
	if annotatedValue == nil || annotatedValue["approvalMessageId"] != prompt.ID {
		t.Fatalf("approval-requested stream event missing approval message id: %#v", annotatedValue)
	}
	if annotatedValue["approvalEventId"] != "$approval" {
		t.Fatalf("approval-requested stream event missing Matrix event target: %#v", annotatedValue)
	}
	annotatedCarriers = splitCarriersForTimedEmission(annotatedCarriers)
	if annotatedNextSeq := aistream.NextSeq(annotatedCarriers); annotatedNextSeq != nextSeq {
		t.Fatalf("approval event target changed stream sequence: initial=%d annotated=%d", nextSeq, annotatedNextSeq)
	}
	choices, ok := annotatedValue["choices"].([]any)
	if !ok || len(choices) != len(aistream.DefaultApprovalChoices()) {
		t.Fatalf("approval-requested stream event missing choices: %#v", annotatedValue["choices"])
	}
	firstChoice, ok := choices[0].(map[string]any)
	if !ok || firstChoice["key"] != aistream.ApprovalChoiceApprove || firstChoice["label"] != "Allow once" {
		t.Fatalf("approval-requested stream event has bad choice shape: %#v", choices[0])
	}

	continuation, err := buildAIApprovalContinuationRun(context.Background(), approvalCtx, agui.ToolApprovalResponse{
		ID:       prompt.ID,
		Approved: true,
	}, time.Unix(20, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(continuation.Prompts) != 0 {
		t.Fatalf("continuation must not request approval again: %#v", continuation.Prompts)
	}
	if continuation.Status.State != "complete" {
		t.Fatalf("approved continuation should finish the run, got %#v", continuation.Status)
	}
	continuationCarriers, err := aistream.PackRunFromSeq(continuation, "$anchor", aistream.CarrierBudgetBytes, approvalCtx.SeqStart)
	if err != nil {
		t.Fatal(err)
	}
	if len(continuationCarriers) == 0 || len(continuationCarriers[0].Envelopes) == 0 || continuationCarriers[0].Envelopes[0].Seq != nextSeq {
		t.Fatalf("continuation should resume at seq %d, got %#v", nextSeq, continuationCarriers)
	}
	if continuation.Events[0]["type"] != agui.EventCustom || continuation.Events[0]["name"] != agui.ApprovalCustomResponded {
		t.Fatalf("continuation must start by acknowledging approval: %#v", continuation.Events)
	}
}

func TestApprovalContinuationResumesOriginalRunAfterApprovedTool(t *testing.T) {
	command := "stream-tools 240 shell#approval fetch --seed=7 --chunk-chars=32:32"
	approvalCtx := aistream.ApprovalContext{
		ID:          "approval-run-1-dummy-tool-1-shell",
		ThreadID:    "thread-1",
		RunID:       "run-1",
		MessageID:   "msg-run-1",
		Command:     command,
		ToolCallID:  "dummy-tool-1-shell",
		ToolName:    "shell",
		TargetEvent: "$anchor",
		AgentID:     "ai",
		AgentName:   "AI",
		SeqStart:    12,
	}
	run, err := buildAIApprovalContinuationRun(context.Background(), approvalCtx, agui.ToolApprovalResponse{
		ID:       approvalCtx.ID,
		Approved: true,
	}, time.Unix(20, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(run.Events) == 0 {
		t.Fatal("expected continuation events")
	}
	if run.Events[0]["type"] != agui.EventCustom || run.Events[0]["name"] != agui.ApprovalCustomResponded {
		t.Fatalf("first continuation event should acknowledge approval, got %#v", run.Events[0])
	}
	seenApprovedTool := false
	seenLaterTool := false
	seenFinished := false
	for _, evt := range run.Events {
		if evt["type"] == agui.EventToolCallEnd && evt["toolCallId"] == approvalCtx.ToolCallID {
			if evt["state"] == agui.ToolStateApprovalResponded {
				result := jsonResultMap(t, evt["result"])
				if result["approved"] != true {
					t.Fatalf("approved result missing approval state: %#v", result)
				}
				seenApprovedTool = true
			}
		}
		if evt["type"] == agui.EventToolCallStart && evt["toolCallId"] == "dummy-tool-2-fetch" {
			seenLaterTool = true
		}
		if evt["type"] == agui.EventRunFinished {
			seenFinished = true
		}
	}
	if !seenApprovedTool || !seenLaterTool || !seenFinished {
		t.Fatalf("continuation did not resume fully: approved=%v laterTool=%v finished=%v events=%#v", seenApprovedTool, seenLaterTool, seenFinished, run.Events)
	}
	if run.Status.State != "complete" {
		t.Fatalf("approved continuation status = %#v", run.Status)
	}
	if len(run.Prompts) != 0 {
		t.Fatalf("finished continuation should not keep pending prompts: %#v", run.Prompts)
	}
}

func TestApprovalContinuationStopsOriginalRunAfterDeniedTool(t *testing.T) {
	command := "stream-tools 240 shell#approval fetch --seed=7 --chunk-chars=32:32"
	approvalCtx := aistream.ApprovalContext{
		ID:          "approval-run-1-dummy-tool-1-shell",
		ThreadID:    "thread-1",
		RunID:       "run-1",
		MessageID:   "msg-run-1",
		Command:     command,
		ToolCallID:  "dummy-tool-1-shell",
		ToolName:    "shell",
		TargetEvent: "$anchor",
		AgentID:     "ai",
		AgentName:   "AI",
		SeqStart:    12,
	}
	run, err := buildAIApprovalContinuationRun(context.Background(), approvalCtx, agui.ToolApprovalResponse{
		ID:       approvalCtx.ID,
		Approved: false,
		Reason:   "denied",
	}, time.Unix(20, 0))
	if err != nil {
		t.Fatal(err)
	}
	seenDeniedTool := false
	for _, evt := range run.Events {
		if evt["type"] == agui.EventToolCallStart && evt["toolCallId"] == "dummy-tool-2-fetch" {
			t.Fatalf("denied approval must not continue later tools: %#v", run.Events)
		}
		if evt["type"] == agui.EventToolCallEnd && evt["toolCallId"] == approvalCtx.ToolCallID && evt["state"] == agui.ToolStateApprovalResponded {
			result := jsonResultMap(t, evt["result"])
			if result["state"] != agui.ToolResultStateError || result["reason"] != "denied" {
				t.Fatalf("bad denied result: %#v", result)
			}
			seenDeniedTool = true
		}
	}
	if !seenDeniedTool {
		t.Fatalf("missing denied approval result: %#v", run.Events)
	}
	if run.Status.State != "error" {
		t.Fatalf("denied continuation status = %#v", run.Status)
	}
}

func TestBuildAIRunToolsDenyProducesStructuredDeniedResult(t *testing.T) {
	run, err := buildAIRun(context.Background(), "run-1", "thread-1", "stream-tools 120 shell#deny --seed=7 --chunk-chars=32:32", time.Unix(10, 0))
	if err != nil {
		t.Fatal(err)
	}
	for _, evt := range run.Events {
		if evt["type"] != agui.EventToolCallEnd {
			continue
		}
		result := jsonResultMap(t, evt["result"])
		if result["state"] == agui.ToolResultStateError && result["reason"] == "denied" {
			return
		}
	}
	t.Fatalf("missing structured denied tool result: %#v", run.Events)
}

func TestBuildAIRunToolsOmitPlaceholderArgsAndEmitTerminalResult(t *testing.T) {
	run, err := buildAIRun(context.Background(), "run-1", "thread-1", "stream-tools 120 shell --seed=7 --chunk-chars=32:32", time.Unix(10, 0))
	if err != nil {
		t.Fatal(err)
	}
	for _, evt := range run.Events {
		if evt["type"] == agui.EventToolCallArgs {
			t.Fatalf("plain demo tool should not emit placeholder args: %#v", evt)
		}
		if evt["type"] == agui.EventToolCallEnd {
			if evt["input"] != nil {
				t.Fatalf("plain demo tool should omit placeholder input: %#v", evt)
			}
			result := jsonResultMap(t, evt["result"])
			if result["state"] != agui.ToolResultStateComplete || result["status"] != "success" {
				t.Fatalf("plain demo tool should emit terminal success result: %#v", evt)
			}
			return
		}
	}
	t.Fatal("missing TOOL_CALL_END event")
}

func TestBuildAIRunToolsPrelimUsesAGUIToolResult(t *testing.T) {
	run, err := buildAIRun(context.Background(), "run-1", "thread-1", "stream-tools 120 shell#prelim --seed=7 --chunk-chars=32:32", time.Unix(10, 0))
	if err != nil {
		t.Fatal(err)
	}
	for _, evt := range run.Events {
		if evt["type"] != agui.EventToolCallResult {
			continue
		}
		if evt["state"] != agui.ToolResultStateStreaming || evt["toolCallId"] == "" || evt["content"] == "" {
			t.Fatalf("bad TOOL_CALL_RESULT event: %#v", evt)
		}
		return
	}
	t.Fatal("missing TOOL_CALL_RESULT event")
}

func TestBuildAIRunFinalSnapshotPreservesToolParts(t *testing.T) {
	run, err := buildAIRun(context.Background(), "run-1", "thread-1", "stream-tools 120 shell#prelim fetch#fail --seed=7 --chunk-chars=32:32", time.Unix(10, 0))
	if err != nil {
		t.Fatal(err)
	}
	var snapshot []agui.UIMessage
	seenRunFinished := false
	for _, evt := range run.Events {
		switch evt["type"] {
		case agui.EventMessagesSnapshot:
			if seenRunFinished {
				t.Fatal("final snapshot must be emitted before RUN_FINISHED")
			}
			var ok bool
			snapshot, ok = evt["messages"].([]agui.UIMessage)
			if !ok {
				t.Fatalf("bad snapshot payload: %#v", evt["messages"])
			}
		case agui.EventRunFinished:
			seenRunFinished = true
		}
	}
	if len(snapshot) != 1 {
		t.Fatalf("expected one final UI message snapshot, got %#v", snapshot)
	}
	seenToolCall := false
	seenToolResult := false
	for _, part := range snapshot[0].Parts {
		switch part["type"] {
		case "tool-call":
			seenToolCall = true
		case "tool-result":
			seenToolResult = true
		}
	}
	if !seenToolCall || !seenToolResult {
		t.Fatalf("final snapshot lost tool parts: %#v", snapshot[0].Parts)
	}
}

func TestBuildAIRunToolsFailureDeltaAndInputError(t *testing.T) {
	run, err := buildAIRun(context.Background(), "run-1", "thread-1", "stream-tools 120 shell#fail fetch#delta parser#inputerror --seed=7 --chunk-chars=8:8", time.Unix(10, 0))
	if err != nil {
		t.Fatal(err)
	}
	seenFailure := false
	seenInputError := false
	for _, evt := range run.Events {
		if evt["type"] != agui.EventToolCallEnd && evt["type"] != agui.EventToolCallArgs {
			continue
		}
		toolCallID, _ := evt["toolCallId"].(string)
		if evt["type"] == agui.EventToolCallArgs && strings.Contains(toolCallID, "fetch") {
			t.Fatalf("delta tool without real input should not emit placeholder args: %#v", evt)
		}
		if evt["type"] == agui.EventToolCallEnd {
			if strings.Contains(toolCallID, "shell") {
				result := jsonResultMap(t, evt["result"])
				if result["state"] == agui.ToolResultStateError {
					seenFailure = true
				}
			}
			if strings.Contains(toolCallID, "parser") {
				result := jsonResultMap(t, evt["result"])
				if result["reason"] == "input-error" {
					seenInputError = true
				}
			}
		}
	}
	if !seenFailure || !seenInputError {
		t.Fatalf("missing tool tag coverage: failure=%v inputError=%v", seenFailure, seenInputError)
	}
}

func TestBuildAIRunToolsProviderTagAddsRawEventPassthrough(t *testing.T) {
	run, err := buildAIRun(context.Background(), "run-1", "thread-1", "stream-tools 120 shell#provider --seed=7 --chunk-chars=32:32", time.Unix(10, 0))
	if err != nil {
		t.Fatal(err)
	}
	for _, evt := range run.Events {
		raw, ok := evt["rawEvent"].(map[string]any)
		if !ok {
			continue
		}
		if raw["provider"] != "dummybridge" || raw["tool"] != "shell" {
			t.Fatalf("bad raw provider event: %#v", raw)
		}
		carriers, err := aistream.PackRun(*run, "$anchor", aistream.CarrierBudgetBytes)
		if err != nil {
			t.Fatal(err)
		}
		if len(carriers) == 0 {
			t.Fatal("expected packed carriers")
		}
		return
	}
	t.Fatal("missing rawEvent for provider-tagged tool")
}

func TestBuildAIRunTerminalErrorAndAbortStates(t *testing.T) {
	errorRun, err := buildAIRun(context.Background(), "run-error", "thread-1", "stream 1 --terminal=error --seed=7 --no-approval", time.Unix(10, 0))
	if err != nil {
		t.Fatal(err)
	}
	if errorRun.Status.State != "error" {
		t.Fatalf("expected error status, got %#v", errorRun.Status)
	}
	abortRun, err := buildAIRun(context.Background(), "run-abort", "thread-1", "stream 1 --terminal=abort --seed=7 --no-approval", time.Unix(10, 0))
	if err != nil {
		t.Fatal(err)
	}
	if abortRun.Status.State != "aborted" {
		t.Fatalf("expected aborted status, got %#v", abortRun.Status)
	}
}

func TestBuildAIRunOver64KBPacksTo58KCarriers(t *testing.T) {
	run, err := buildAIRun(context.Background(), "run-1", "thread-1", "stream 1 --chars=70000 --actions=1 --seed=7", time.Unix(10, 0))
	if err != nil {
		t.Fatal(err)
	}
	carriers, err := aistream.PackRun(*run, "$anchor", aistream.CarrierBudgetBytes)
	if err != nil {
		t.Fatal(err)
	}
	if len(carriers) < 2 {
		t.Fatalf("expected split carriers, got %d", len(carriers))
	}
	for i, carrier := range carriers {
		if size := aistream.JSONSize(aistream.CarrierContent(carrier.Envelopes)); size > aistream.CarrierBudgetBytes {
			t.Fatalf("carrier %d size = %d", i, size)
		}
	}
	for _, carrier := range carriers {
		for _, envelope := range carrier.Envelopes {
			if envelope.Part["type"] != agui.EventMessagesSnapshot {
				continue
			}
			raw, err := json.Marshal(envelope.Part)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), strings.Repeat("a", 60*1024)) {
				t.Fatal("final snapshot should not repeat full streamed text")
			}
		}
	}
	if len(aistream.ReconstructText(carriers)) < 60*1024 {
		t.Fatalf("expected large reconstructed output, got %d", len(aistream.ReconstructText(carriers)))
	}
}

func TestBuildAIRunPlansChaosCreatesMultipleRuns(t *testing.T) {
	plans, err := buildAIRunPlans(context.Background(), "run-chaos", "thread-1", "stream 1 --runs=3 --actions=3 --seed=7 --stagger-ms=1:1", time.Unix(10, 0), "ai", "AI")
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 3 {
		t.Fatalf("expected three chaos runs, got %d", len(plans))
	}
	seen := map[string]bool{}
	for i, plan := range plans {
		if plan.Run == nil {
			t.Fatalf("nil run at %d", i)
		}
		if seen[plan.Run.RunID] {
			t.Fatalf("duplicate run ID %q", plan.Run.RunID)
		}
		seen[plan.Run.RunID] = true
		if plan.Run.ThreadID != "thread-1" {
			t.Fatalf("bad thread ID: %q", plan.Run.ThreadID)
		}
		if i > 0 && plan.Delay <= 0 {
			t.Fatalf("expected nonzero child stagger delay, got %#v", plans)
		}
	}
}

func TestBuildAIRunRandomHonorsVirtualDelays(t *testing.T) {
	run, err := buildAIRun(context.Background(), "run-1", "thread-1", "stream 3 --actions=4 --seed=7 --delay-ms=100:100", time.Unix(10, 0))
	if err != nil {
		t.Fatal(err)
	}
	var first, last int64
	for _, evt := range run.Events {
		ts, _ := evt["timestamp"].(int64)
		if ts == 0 {
			if n, ok := evt["timestamp"].(int); ok {
				ts = int64(n)
			}
		}
		if ts == 0 {
			continue
		}
		if first == 0 {
			first = ts
		}
		last = ts
	}
	if first == 0 || last-first < 300 {
		t.Fatalf("expected random run timestamps to reflect action delays, first=%d last=%d", first, last)
	}
}

func TestRandomModeApprovalPause(t *testing.T) {
	for seed := int64(1); seed <= 200; seed++ {
		run, err := buildAIRun(context.Background(), "run-approval", "thread-approval", "stream 1 --profile=tools --seed="+strconv.FormatInt(seed, 10), time.Unix(10, 0))
		if err != nil {
			t.Fatal(err)
		}
		if run.ApprovalID == "" {
			continue
		}
		for _, evt := range run.Events {
			if evt["type"] == agui.EventRunFinished {
				t.Fatalf("approval run emitted RUN_FINISHED with seed %d", seed)
			}
		}
		if run.Status.State != "streaming" {
			t.Fatalf("expected approval run to remain streaming, got %q", run.Status.State)
		}
		return
	}
	t.Fatal("no approval action selected for tested random seeds")
}

func TestBalancedStream50UsuallyPausesForApproval(t *testing.T) {
	for seed := int64(1); seed <= 20; seed++ {
		run, err := buildAIRun(context.Background(), "run-approval", "thread-approval", "stream 50 --seed="+strconv.FormatInt(seed, 10), time.Unix(10, 0))
		if err != nil {
			t.Fatal(err)
		}
		if run.ApprovalID != "" {
			return
		}
	}
	t.Fatal("balanced stream 50 did not request approval for any sampled seed")
}

func TestBalancedStream50DoesNotPauseImmediatelyForApproval(t *testing.T) {
	run, err := buildAIRun(context.Background(), "run-approval", "thread-approval", "stream 50 --seed=1", time.Unix(10, 0))
	if err != nil {
		t.Fatal(err)
	}
	if run.ApprovalID == "approval-run-approval-dummy-tool-1-calendar" {
		t.Fatalf("balanced stream paused immediately for approval: %q", run.ApprovalID)
	}
	if strings.Contains(run.ApprovalID, "dummy-tool-1-") {
		t.Fatalf("balanced stream paused on first action approval: %q", run.ApprovalID)
	}
}

func TestRandomProfilesCoverToolsArtifactsAndTransientData(t *testing.T) {
	balanced := randomCommand{
		sharedStreamOptions: sharedStreamOptions{
			Profile:       "balanced",
			AllowApproval: true,
		},
	}
	seen := map[string]bool{}
	rng := rand.New(rand.NewSource(2))
	for range 400 {
		options, total := buildRandomActionOptions(balanced)
		seen[pickWeighted(options, total, rng)] = true
	}
	if seen[randomActionToolApproval] {
		t.Fatalf("balanced profile should keep approvals rare via tool-call promotion, seen=%#v", seen)
	}

	cmd := randomCommand{
		sharedStreamOptions: sharedStreamOptions{
			Profile:       "tools",
			AllowApproval: true,
		},
	}
	seen = map[string]bool{}
	rng = rand.New(rand.NewSource(4))
	for range 400 {
		options, total := buildRandomActionOptions(cmd)
		seen[pickWeighted(options, total, rng)] = true
	}
	for _, action := range []string{randomActionTool, randomActionToolFail, randomActionToolDeny, randomActionToolApproval} {
		if !seen[action] {
			t.Fatalf("tools profile never selected %s; seen=%#v", action, seen)
		}
	}

	cmd.Profile = "artifacts"
	seen = map[string]bool{}
	rng = rand.New(rand.NewSource(8))
	for range 400 {
		options, total := buildRandomActionOptions(cmd)
		seen[pickWeighted(options, total, rng)] = true
	}
	for _, action := range []string{randomActionSource, randomActionDocument, randomActionFile, randomActionMetadata, randomActionData, randomActionDataTransient} {
		if !seen[action] {
			t.Fatalf("artifacts profile never selected %s; seen=%#v", action, seen)
		}
	}
}

func TestRandomTerminalUsesAllowedOutcomes(t *testing.T) {
	cmd := randomCommand{sharedStreamOptions: sharedStreamOptions{AllowAbort: true, AllowError: true}}
	seen := map[string]bool{}
	rng := rand.New(rand.NewSource(10))
	for range 80 {
		seen[chooseRandomTerminal(cmd, rng)] = true
	}
	for _, terminal := range []string{"finish", "abort", "error"} {
		if !seen[terminal] {
			t.Fatalf("terminal %s was never selected; seen=%#v", terminal, seen)
		}
	}

	if terminal := chooseRandomTerminal(randomCommand{}, rand.New(rand.NewSource(1))); terminal != "finish" {
		t.Fatalf("unexpected terminal without flags: %q", terminal)
	}
}

func TestBuildDemoVisibleTextIsMarkdownRichAndDeterministic(t *testing.T) {
	first := buildDemoVisibleText(420, rand.New(rand.NewSource(7)))
	second := buildDemoVisibleText(420, rand.New(rand.NewSource(7)))
	if first != second {
		t.Fatalf("expected deterministic output for seed")
	}
	for _, signal := range []string{"[", "](", "**", "\n- ", "\n> ", "```", "\n| "} {
		if strings.Contains(first, signal) {
			return
		}
	}
	t.Fatalf("expected markdown-rich text, got %q", first)
}

func TestBuildDemoVisibleTextDoesNotCutMarkdownSyntax(t *testing.T) {
	for _, chars := range []int{24, 40, 60, 80, 96, 120, 180, 260, 420} {
		for seed := int64(1); seed <= 80; seed++ {
			text := buildDemoVisibleText(chars, rand.New(rand.NewSource(seed)))
			if strings.Count(text, "[") != strings.Count(text, "]") {
				t.Fatalf("unbalanced brackets for chars=%d seed=%d: %q", chars, seed, text)
			}
			assertCompleteMarkdownLinks(t, chars, seed, text)
			if strings.Count(text, "```")%2 != 0 {
				t.Fatalf("unbalanced code fence for chars=%d seed=%d: %q", chars, seed, text)
			}
			if strings.Contains(text, "https://dummybridge.") && !strings.Contains(text, "https://dummybridge.local/") {
				t.Fatalf("cut markdown URL for chars=%d seed=%d: %q", chars, seed, text)
			}
		}
	}
}

func TestRandomStreamTextBlocksKeepMarkdownBoundaries(t *testing.T) {
	for seed := int64(1); seed <= 80; seed++ {
		run, err := buildAIRun(context.Background(), "run-markdown", "thread-markdown", "stream 40 --seed="+strconv.FormatInt(seed, 10)+" --no-approval", time.Unix(10, 0))
		if err != nil {
			t.Fatal(err)
		}
		text := run.Text()
		assertCompleteMarkdownLinks(t, 40, seed, text)
		if strings.Count(text, "[") != strings.Count(text, "]") {
			t.Fatalf("unbalanced brackets for seed=%d: %q", seed, text)
		}
		if strings.Count(text, "```")%2 != 0 {
			t.Fatalf("unbalanced code fence for seed=%d: %q", seed, text)
		}
		if joinedMarkdownBlockRE.MatchString(text) {
			t.Fatalf("markdown block joined to previous text for seed=%d: %q", seed, text)
		}
	}
}

var joinedMarkdownBlockRE = regexp.MustCompile(`[[:lower:]](Use |Review the \[|> )`)

func assertCompleteMarkdownLinks(t *testing.T, chars int, seed int64, text string) {
	t.Helper()
	offset := 0
	for {
		start := strings.Index(text[offset:], "](")
		if start < 0 {
			return
		}
		start += offset
		close := strings.IndexByte(text[start+2:], ')')
		if close < 0 {
			t.Fatalf("unclosed markdown link for chars=%d seed=%d: %q", chars, seed, text)
		}
		linkTarget := text[start+2 : start+2+close]
		if strings.ContainsAny(linkTarget, " \n\t") {
			t.Fatalf("cut markdown link for chars=%d seed=%d: %q", chars, seed, text)
		}
		offset = start + 3 + close
	}
}

func TestMultiApprovalContinuationKeepsLaterPrompts(t *testing.T) {
	command := "stream-tools 240 shell#approval fetch#approval --seed=7 --chunk-chars=32:32"
	approvalCtx := aistream.ApprovalContext{
		ID:          "approval-run-1-dummy-tool-1-shell",
		ThreadID:    "thread-1",
		RunID:       "run-1",
		MessageID:   "msg-run-1",
		Command:     command,
		ToolCallID:  "dummy-tool-1-shell",
		ToolName:    "shell",
		TargetEvent: "$anchor",
		AgentID:     "ai",
		AgentName:   "AI",
		SeqStart:    12,
	}
	run, err := buildAIApprovalContinuationRun(context.Background(), approvalCtx, agui.ToolApprovalResponse{
		ID:       approvalCtx.ID,
		Approved: true,
	}, time.Unix(20, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(run.Prompts) != 1 {
		t.Fatalf("expected second approval prompt to be preserved, got %#v", run.Prompts)
	}
	if run.Prompts[0].ToolName != "fetch" {
		t.Fatalf("expected preserved prompt to belong to fetch, got %#v", run.Prompts[0])
	}
	if run.Status.State != "streaming" {
		t.Fatalf("expected continuation with pending approval to remain streaming, got %#v", run.Status)
	}

	secondCtx := aistream.ApprovalContext{
		ID:          run.Prompts[0].ID,
		ThreadID:    approvalCtx.ThreadID,
		RunID:       approvalCtx.RunID,
		MessageID:   approvalCtx.MessageID,
		Command:     command,
		ToolCallID:  run.Prompts[0].ToolCallID,
		ToolName:    run.Prompts[0].ToolName,
		TargetEvent: approvalCtx.TargetEvent,
		AgentID:     approvalCtx.AgentID,
		AgentName:   approvalCtx.AgentName,
		SeqStart:    100,
	}
	finished, err := buildAIApprovalContinuationRunWithApprovals(context.Background(), secondCtx, map[string]agui.ToolApprovalResponse{
		approvalCtx.ID: {
			ID:       approvalCtx.ID,
			Approved: true,
		},
		secondCtx.ID: {
			ID:       secondCtx.ID,
			Approved: true,
		},
	}, time.Unix(30, 0))
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status.State != "complete" {
		t.Fatalf("second approval continuation should finish, got %#v", finished.Status)
	}
	if len(finished.Prompts) != 0 {
		t.Fatalf("finished continuation should not keep prompts: %#v", finished.Prompts)
	}
}

func TestApprovalContinuationReplaysRandomRunWithImplicitSeed(t *testing.T) {
	// Iterate clocks until the random-action profile produces an approval
	// request — the seed is implicit (resolved from now()), and the bug being
	// guarded against is that the continuation would otherwise pick a fresh
	// seed and lose the original toolCallID.
	for tick := int64(1); tick <= 500; tick++ {
		now := time.Unix(tick, 0)
		plans, err := buildAIRunPlans(context.Background(), "run-rand", "thread-rand", "stream 1 --profile=tools", now, "ai", "AI")
		if err != nil {
			t.Fatal(err)
		}
		if len(plans) != 1 || plans[0].Run == nil {
			t.Fatalf("expected one random plan, got %#v", plans)
		}
		originalRun := plans[0].Run
		if originalRun.ApprovalID == "" {
			continue
		}
		if !strings.Contains(plans[0].EffectiveCommand, "--seed=") {
			t.Fatalf("effective command must include resolved seed: %q", plans[0].EffectiveCommand)
		}
		approvalCtx := aistream.ApprovalContext{
			ID:          originalRun.ApprovalID,
			ThreadID:    originalRun.ThreadID,
			RunID:       originalRun.RunID,
			MessageID:   originalRun.MessageID,
			Command:     plans[0].EffectiveCommand,
			ToolCallID:  originalRun.ToolCallID,
			TargetEvent: "$anchor",
			AgentID:     "ai",
			AgentName:   "AI",
			SeqStart:    50,
		}
		continuation, err := buildAIApprovalContinuationRun(context.Background(), approvalCtx, agui.ToolApprovalResponse{
			ID:       approvalCtx.ID,
			Approved: true,
		}, now.Add(time.Hour))
		if err != nil {
			t.Fatalf("continuation failed: %v", err)
		}
		if len(continuation.Events) == 0 {
			t.Fatalf("expected continuation events for random run, got none")
		}
		if continuation.Events[0]["type"] != agui.EventCustom || continuation.Events[0]["name"] != agui.ApprovalCustomResponded {
			t.Fatalf("first continuation event should acknowledge approval, got %#v", continuation.Events[0])
		}
		return
	}
	t.Fatal("no implicit-seed random run produced an approval prompt in the tested range")
}

func TestChaosSubRunCommandIsParseable(t *testing.T) {
	plans, err := buildAIRunPlans(context.Background(), "run-chaos", "thread-chaos", "stream 1 --runs=2 --seed=11", time.Unix(0, 0), "ai", "AI")
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 2 {
		t.Fatalf("expected two chaos sub-runs, got %d", len(plans))
	}
	for i, plan := range plans {
		if !strings.HasPrefix(plan.EffectiveCommand, "stream ") {
			t.Fatalf("chaos plan %d must render as stream, got %q", i, plan.EffectiveCommand)
		}
		if !strings.Contains(plan.EffectiveCommand, "--seed=") {
			t.Fatalf("chaos sub-run command must include explicit seed: %q", plan.EffectiveCommand)
		}
		cmd, err := parseCommand(plan.EffectiveCommand)
		if err != nil {
			t.Fatalf("chaos sub-run command did not re-parse: %v (%q)", err, plan.EffectiveCommand)
		}
		if cmd == nil || cmd.Random == nil || !cmd.Random.SeedSet {
			t.Fatalf("re-parsed chaos sub-run lost seed: %#v", cmd)
		}
	}
}

func jsonResultMap(t *testing.T, value any) map[string]any {
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
