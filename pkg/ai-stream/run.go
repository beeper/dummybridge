package aistream

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/beeper/dummybridge/pkg/ag-ui"
)

const (
	BeeperAIKey          = "com.beeper.ai"
	BeeperAIMetadataKey  = "com.beeper.ai.metadata"
	BeeperAIStreamKey    = "com.beeper.llm"
	BeeperAIStreamDeltas = BeeperAIStreamKey + ".deltas"
	FinalPartsCustomName = "com.beeper.ai.final-parts"
	DefaultModel         = "dummybridge/ag-ui"
	CarrierBudgetBytes   = 40 * 1024
	PreviewBudgetBytes   = 4096
	SnapshotTextBytes    = 4096
)

type Run struct {
	ThreadID   string
	RunID      string
	MessageID  string
	Model      string
	AgentID    string
	AgentName  string
	Events     []agui.Event
	Approvals  []ApprovalSummary
	Artifacts  ArtifactSummary
	Data       map[string]any
	Status     Status
	Usage      agui.Usage
	Preview    Preview
	ToolCallID string
	ApprovalID string
	Prompts    []ApprovalPrompt
}

type Status struct {
	State        string `json:"state"`
	FinishReason string `json:"finishReason,omitempty"`
	Terminal     any    `json:"terminal"`
	Error        any    `json:"error"`
}

type Preview struct {
	Text      string `json:"text"`
	Truncated bool   `json:"truncated"`
}

type UIMessageMetadata struct {
	ThreadID string      `json:"threadId"`
	RunID    string      `json:"runId"`
	Status   Status      `json:"status"`
	Usage    *agui.Usage `json:"usage,omitempty"`
}

func (m UIMessageMetadata) Map() map[string]any {
	out := map[string]any{
		"threadId": m.ThreadID,
		"runId":    m.RunID,
		"status":   m.Status,
	}
	if m.Usage != nil {
		out["usage"] = *m.Usage
	}
	return out
}

type RunMetadata struct {
	Schema    string
	Protocol  string
	ThreadID  string
	RunID     string
	MessageID string
	AgentID   string
	AgentName string
	Model     string
	Usage     agui.Usage
	Status    Status
	Approvals []ApprovalSummary
	Artifacts ArtifactSummary
	Data      map[string]any
	Preview   Preview
}

func (m RunMetadata) Map() map[string]any {
	return map[string]any{
		"schema":    m.Schema,
		"protocol":  m.Protocol,
		"threadId":  m.ThreadID,
		"runId":     m.RunID,
		"messageId": m.MessageID,
		"agent": map[string]any{
			"id":          m.AgentID,
			"displayName": m.AgentName,
		},
		"model": m.Model,
		"usage": map[string]any{
			"promptTokens":     m.Usage.PromptTokens,
			"completionTokens": m.Usage.CompletionTokens,
			"totalTokens":      m.Usage.TotalTokens,
		},
		"usageDetails": map[string]any{},
		"status":       m.Status,
		"approvals":    m.Approvals,
		"artifacts":    m.Artifacts,
		"data":         m.Data,
		"preview":      m.Preview,
	}
}

type ApprovalSummary struct {
	ID         string         `json:"id"`
	ToolCallID string         `json:"toolCallId"`
	State      string         `json:"state"`
	Always     bool           `json:"always"`
	Reason     string         `json:"reason,omitempty"`
	Fields     map[string]any `json:"fields,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type ApprovalPrompt struct {
	ID         string
	ToolCallID string
	ToolName   string
	SeqStart   int
}

type ArtifactSummary struct {
	Sources   []map[string]any `json:"sources"`
	Documents []map[string]any `json:"documents"`
	Files     []map[string]any `json:"files"`
}

type Writer struct {
	Run           *Run
	builder       agui.EventBuilder
	reasoningOpen bool
}

func NewRun(runID, threadID, model, agentID, agentName string, now time.Time) *Run {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		runID = fmt.Sprintf("run-%d", now.UnixNano())
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		threadID = runID
	}
	model = strings.TrimSpace(model)
	if model == "" {
		model = DefaultModel
	}
	if agentID == "" {
		agentID = "ai"
	}
	if agentName == "" {
		agentName = "AI"
	}
	run := &Run{
		ThreadID:  threadID,
		RunID:     runID,
		MessageID: "msg-" + runID,
		Model:     model,
		AgentID:   agentID,
		AgentName: agentName,
		Data:      map[string]any{},
		Status:    Status{State: "streaming"},
	}
	run.Preview = Preview{Text: BoundedPreview("", PreviewBudgetBytes)}
	return run
}

func NewWriter(run *Run, now func() time.Time) *Writer {
	return &Writer{Run: run, builder: agui.NewEventBuilder(run.Model, now)}
}

func (w *Writer) Add(evt agui.Event) {
	if w == nil || w.Run == nil || len(evt) == 0 {
		return
	}
	w.Run.Events = append(w.Run.Events, evt)
	w.applySummary(evt)
}

func (w *Writer) Start() {
	w.Add(w.builder.RunStarted(w.Run.ThreadID, w.Run.RunID))
	w.Add(w.builder.TextMessageStart(w.Run.MessageID, agui.RoleAssistant))
}

func (w *Writer) Text(delta string) {
	if delta == "" {
		return
	}
	w.Add(w.builder.TextMessageContent(w.Run.MessageID, delta))
}

func (w *Writer) Thinking(delta string) {
	if delta == "" {
		return
	}
	if !w.reasoningOpen {
		w.Add(w.builder.ReasoningStart(w.Run.MessageID))
		w.Add(w.builder.ReasoningMessageStart(w.Run.MessageID))
		w.reasoningOpen = true
	}
	w.Add(w.builder.ReasoningMessageContent(w.Run.MessageID, delta))
}

func (w *Writer) StepStart(stepID string) {
	w.Add(w.builder.StepStarted(w.Run.MessageID, stepID))
}

func (w *Writer) StepFinish(stepID string) {
	w.Add(w.builder.StepFinished(w.Run.MessageID, stepID))
}

func (w *Writer) ToolStart(toolCallID, name string, index int, approval *agui.ToolApproval) {
	w.ToolStartWithMetadata(toolCallID, name, index, approval, nil)
}

func (w *Writer) ToolStartWithMetadata(toolCallID, name string, index int, approval *agui.ToolApproval, metadata map[string]any) {
	idx := index
	w.Add(w.builder.ToolCallStartWithMetadata(w.Run.MessageID, toolCallID, name, &idx, approval, metadata))
	if approval != nil {
		w.recordApprovalRequest(toolCallID, name, approval)
	}
}

func (w *Writer) ToolApprovalRequested(toolCallID, name string, input any, approval agui.ToolApproval) {
	w.ToolApprovalRequestedWithMetadata(toolCallID, name, input, approval, nil)
}

func (w *Writer) ToolApprovalRequestedWithMetadata(toolCallID, name string, input any, approval agui.ToolApproval, metadata map[string]any) {
	w.recordApprovalRequest(toolCallID, name, &approval)
	value := NewApprovalRequestedValue(*w.Run, toolCallID, name, input, approval)
	value.Metadata = metadata
	w.Add(w.builder.Custom(
		agui.ApprovalCustomRequested,
		value.Map(),
	))
}

func (w *Writer) recordApprovalRequest(toolCallID, name string, approval *agui.ToolApproval) {
	if approval == nil || approval.ID == "" {
		return
	}
	w.Run.ToolCallID = toolCallID
	w.Run.ApprovalID = approval.ID
	for _, existing := range w.Run.Approvals {
		if existing.ID == approval.ID {
			return
		}
	}
	w.Run.Approvals = append(w.Run.Approvals, ApprovalSummary{
		ID:         approval.ID,
		ToolCallID: toolCallID,
		State:      "requested",
	})
	w.Run.Prompts = append(w.Run.Prompts, ApprovalPrompt{ID: approval.ID, ToolCallID: toolCallID, ToolName: name})
}

func (w *Writer) ToolArgs(toolCallID, delta string, args any) {
	w.Add(w.builder.ToolCallArgs(toolCallID, delta, args))
}

func (w *Writer) ToolEnd(toolCallID, name string, input, result any) {
	w.Add(w.builder.ToolCallEnd(toolCallID, name, input, jsonString(result), agui.ToolStateInputComplete))
}

func (w *Writer) ToolApprovalInputComplete(toolCallID, name string, input any) {
	w.Add(w.builder.ToolCallEnd(toolCallID, name, input, nil, agui.ToolStateApprovalRequested))
}

func (w *Writer) ToolApprovalResponded(toolCallID, name string, input any, response agui.ToolApprovalResponse) {
	for i := range w.Run.Approvals {
		if w.Run.Approvals[i].ID == response.ID {
			w.Run.Approvals[i].State = approvalSummaryState(response)
			w.Run.Approvals[i].Always = response.Always
			w.Run.Approvals[i].Reason = response.Reason
			w.Run.Approvals[i].Fields = response.Fields
			w.Run.Approvals[i].Metadata = response.Metadata
		}
	}
	w.Add(w.builder.Custom(agui.ApprovalCustomResponded, map[string]any{
		"threadId":   w.Run.ThreadID,
		"runId":      w.Run.RunID,
		"messageId":  w.Run.MessageID,
		"toolCallId": toolCallID,
		"toolName":   name,
		"approval":   response,
	}))
	result := map[string]any{
		"approvalId": response.ID,
		"always":     response.Always,
	}
	if response.Fields != nil {
		result["fields"] = response.Fields
	}
	if response.Metadata != nil {
		result["metadata"] = response.Metadata
	}
	if response.Approved {
		result["state"] = agui.ToolResultStateComplete
		result["status"] = "success"
		result["approved"] = true
	} else {
		reason := response.Reason
		if reason == "" {
			reason = "denied"
		}
		result["state"] = agui.ToolResultStateError
		result["status"] = "denied"
		result["reason"] = reason
	}
	w.Add(w.builder.ToolCallEnd(toolCallID, name, input, jsonString(result), agui.ToolStateApprovalResponded))
}

func (w *Writer) ToolResult(toolCallID, content, state string) {
	w.Add(w.builder.ToolCallResult(w.Run.MessageID, toolCallID, content, state, agui.RoleTool))
}

func (w *Writer) ToolError(toolCallID, name string, input any, reason string) {
	w.Add(w.builder.ToolCallEnd(toolCallID, name, input, jsonString(map[string]any{
		"state":  agui.ToolResultStateError,
		"status": "failed",
		"reason": reason,
	}), agui.ToolStateInputComplete))
}

func (w *Writer) ToolDenied(toolCallID, name string, input any, approvalID, reason string) {
	if reason == "" {
		reason = "denied"
	}
	for i := range w.Run.Approvals {
		if w.Run.Approvals[i].ID == approvalID {
			w.Run.Approvals[i].State = "denied"
			w.Run.Approvals[i].Reason = reason
		}
	}
	w.Add(w.builder.Custom(agui.ApprovalCustomResponded, map[string]any{
		"approval": agui.ToolApprovalResponse{ID: approvalID, Approved: false, Reason: reason},
	}))
	w.Add(w.builder.ToolCallEnd(toolCallID, name, input, jsonString(map[string]any{
		"state":  agui.ToolResultStateError,
		"status": "denied",
		"reason": reason,
	}), agui.ToolStateApprovalResponded))
}

func jsonString(value any) any {
	if value == nil {
		return nil
	}
	if text, ok := value.(string); ok {
		return text
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(raw)
}

func (w *Writer) StateSnapshot(state map[string]any) {
	w.Add(w.builder.StateSnapshot(state))
}

func (w *Writer) StateDelta(delta any) {
	w.Add(w.builder.StateDelta(delta))
}

func (w *Writer) MessagesSnapshot(messages []agui.UIMessage) {
	w.Add(w.builder.MessagesSnapshot(messages))
}

func (w *Writer) Custom(name string, value any) {
	w.Add(w.builder.Custom(name, value))
}

func (w *Writer) Finish(reason string) {
	reason = agui.NormalizeFinishReason(reason)
	text := w.Run.Text()
	w.finishReasoning()
	w.Run.Usage = agui.Usage{
		PromptTokens:     1,
		CompletionTokens: utf8.RuneCountInString(text),
		TotalTokens:      utf8.RuneCountInString(text) + 1,
	}
	w.Run.Status = Status{State: "complete", FinishReason: reason}
	w.Add(w.builder.TextMessageEnd(w.Run.MessageID))
	w.addFinalSnapshot()
	w.Add(w.builder.RunFinished(w.Run.ThreadID, w.Run.RunID, reason, w.Run.Usage))
}

func (w *Writer) Error(message string) {
	w.finishReasoning()
	w.Run.Status = Status{State: "error", Error: map[string]any{"message": message}}
	w.addFinalSnapshot()
	w.Add(w.builder.RunError(w.Run.ThreadID, w.Run.RunID, message))
}

func (w *Writer) Abort(message string) {
	w.finishReasoning()
	w.Run.Status = Status{State: "aborted", Error: map[string]any{"message": message}}
	w.addFinalSnapshot()
	w.Add(w.builder.RunError(w.Run.ThreadID, w.Run.RunID, message))
}

func (w *Writer) addFinalSnapshot() {
	if w == nil || w.Run == nil {
		return
	}
	w.MessagesSnapshot([]agui.UIMessage{w.Run.FinalUIMessage(0, true)})
}

func (w *Writer) finishReasoning() {
	if !w.reasoningOpen {
		return
	}
	w.Add(w.builder.ReasoningMessageEnd(w.Run.MessageID))
	w.Add(w.builder.ReasoningEnd(w.Run.MessageID))
	w.reasoningOpen = false
}

func (w *Writer) applySummary(evt agui.Event) {
	switch evt["type"] {
	case agui.EventTextMessageContent:
		if delta, _ := evt["delta"].(string); delta != "" {
			w.Run.Preview = PreviewFromText(w.Run.Text(), PreviewBudgetBytes)
		}
	case agui.EventCustom:
		name, _ := evt["name"].(string)
		value, _ := evt["value"].(map[string]any)
		switch name {
		case "com.beeper.source":
			w.Run.Artifacts.Sources = append(w.Run.Artifacts.Sources, value)
		case "com.beeper.document":
			w.Run.Artifacts.Documents = append(w.Run.Artifacts.Documents, value)
		case "com.beeper.file":
			w.Run.Artifacts.Files = append(w.Run.Artifacts.Files, value)
		case "com.beeper.data":
			if key, _ := value["name"].(string); key != "" {
				w.Run.Data[key] = value["value"]
			}
		}
	}
}

func (t Run) Text() string {
	var out strings.Builder
	for _, evt := range t.Events {
		if evt["type"] == agui.EventTextMessageContent {
			if delta, _ := evt["delta"].(string); delta != "" {
				out.WriteString(delta)
			}
		}
	}
	return out.String()
}

func (t Run) FinalUIMessage(textBudget int, includeThinking bool) agui.UIMessage {
	message := agui.UIMessage{
		ID:       t.MessageID,
		Role:     agui.RoleAssistant,
		Metadata: t.UIMessageMetadata(true).Map(),
	}
	var textPart agui.MessagePart
	var thinkingPart agui.MessagePart
	var textContent, thinkingContent strings.Builder
	toolParts := map[string]agui.MessagePart{}
	toolResultParts := map[string]agui.MessagePart{}
	approvalByID := map[string]any{}
	appendPart := func(part agui.MessagePart) agui.MessagePart {
		message.Parts = append(message.Parts, part)
		return part
	}
	for _, evt := range t.Events {
		switch evt["type"] {
		case agui.EventTextMessageContent:
			delta, _ := evt["delta"].(string)
			if delta == "" {
				continue
			}
			if textPart == nil {
				textPart = appendPart(agui.MessagePart{"type": "text", "content": "", "state": agui.PartStateStreaming})
			}
			textContent.WriteString(delta)
		case agui.EventTextMessageEnd:
			if textPart != nil {
				textPart["state"] = agui.PartStateDone
			}
		case agui.EventReasoningMsgCont:
			delta, _ := evt["delta"].(string)
			if delta == "" {
				continue
			}
			if !includeThinking {
				continue
			}
			if thinkingPart == nil {
				thinkingPart = appendPart(agui.MessagePart{"type": "thinking", "content": "", "state": agui.PartStateStreaming})
			}
			thinkingContent.WriteString(delta)
		case agui.EventReasoningMsgEnd:
			if thinkingPart != nil {
				thinkingPart["state"] = agui.PartStateDone
			}
		case agui.EventToolCallStart:
			toolCallID, _ := evt["toolCallId"].(string)
			if toolCallID == "" {
				continue
			}
			part := agui.MessagePart{
				"type":       "tool-call",
				"id":         toolCallID,
				"toolCallId": toolCallID,
				"name":       firstString(evt["toolName"], evt["toolCallName"]),
				"arguments":  "",
				"state":      firstString(evt["state"]),
			}
			if index, ok := evt["index"]; ok {
				part["index"] = index
			}
			if approval, ok := evt["approval"]; ok {
				part["approval"] = approval
			}
			if metadata, ok := evt["metadata"]; ok {
				part["metadata"] = metadata
			}
			toolParts[toolCallID] = appendPart(part)
		case agui.EventToolCallArgs:
			toolCallID, _ := evt["toolCallId"].(string)
			part := toolParts[toolCallID]
			if part == nil {
				part = appendPart(agui.MessagePart{"type": "tool-call", "id": toolCallID, "toolCallId": toolCallID, "arguments": ""})
				toolParts[toolCallID] = part
			}
			part["state"] = firstString(evt["state"])
			if delta, _ := evt["delta"].(string); delta != "" {
				part["arguments"] = asString(part["arguments"]) + delta
			}
			if args, ok := evt["args"]; ok {
				part["input"] = args
			}
		case agui.EventToolCallEnd:
			toolCallID, _ := evt["toolCallId"].(string)
			part := toolParts[toolCallID]
			if part == nil {
				part = appendPart(agui.MessagePart{"type": "tool-call", "id": toolCallID, "toolCallId": toolCallID})
				toolParts[toolCallID] = part
			}
			part["name"] = firstString(part["name"], evt["toolName"], evt["toolCallName"])
			part["state"] = firstString(evt["state"])
			if input, ok := evt["input"]; ok {
				part["input"] = input
			}
			if result, ok := evt["result"]; ok {
				part["output"] = result
			}
		case agui.EventToolCallResult:
			toolCallID, _ := evt["toolCallId"].(string)
			if toolCallID == "" {
				continue
			}
			part := toolResultParts[toolCallID]
			if part == nil {
				part = appendPart(agui.MessagePart{"type": "tool-result", "toolCallId": toolCallID, "content": "", "state": firstString(evt["state"])})
				toolResultParts[toolCallID] = part
			}
			part["state"] = firstString(evt["state"])
			part["content"] = asString(part["content"]) + asString(evt["content"])
		case agui.EventCustom:
			name, _ := evt["name"].(string)
			value, _ := evt["value"].(map[string]any)
			switch name {
			case agui.ApprovalCustomRequested:
				if toolCallID, _ := value["toolCallId"].(string); toolCallID != "" {
					if part := toolParts[toolCallID]; part != nil {
						part["approval"] = value["approval"]
						part["state"] = agui.ToolStateApprovalRequested
					}
				}
			case agui.ApprovalCustomResponded:
				if approval, ok := value["approval"]; ok {
					approvalByID[approvalMapID(approval)] = approval
				}
			case "com.beeper.source":
				message.Parts = append(message.Parts, agui.MessagePart{"type": "source-url", "source": value})
			case "com.beeper.document":
				message.Parts = append(message.Parts, agui.MessagePart{"type": "file", "file": value})
			case "com.beeper.file":
				message.Parts = append(message.Parts, agui.MessagePart{"type": "file", "file": value})
			case "com.beeper.data":
				message.Parts = append(message.Parts, agui.MessagePart{"type": "data-com-beeper-data", "data": value})
			}
		}
	}
	for _, part := range toolParts {
		if approvalID := approvalMapID(part["approval"]); approvalID != "" {
			if response := approvalByID[approvalID]; response != nil {
				part["approvalResponse"] = response
				part["state"] = agui.ToolStateApprovalResponded
			}
		}
	}
	if textPart != nil {
		textPart["content"] = textContent.String()
	}
	if thinkingPart != nil {
		thinkingPart["content"] = thinkingContent.String()
	}
	compactTextPart(textPart, textBudget)
	compactTextPart(thinkingPart, textBudget)
	if len(message.Parts) > 1 {
		visible := make([]agui.MessagePart, 0, len(message.Parts))
		other := make([]agui.MessagePart, 0, len(message.Parts))
		for _, part := range message.Parts {
			switch part["type"] {
			case "text", "thinking":
				visible = append(visible, part)
			default:
				other = append(other, part)
			}
		}
		if len(visible) > 0 {
			message.Parts = append(visible, other...)
		}
	}
	return message
}

func (t Run) InitialUIMessage() agui.UIMessage {
	message := agui.UIMessage{
		ID:       t.MessageID,
		Role:     agui.RoleAssistant,
		Metadata: t.UIMessageMetadata(false).Map(),
	}
	if t.Preview.Text != "" {
		message.Parts = []agui.MessagePart{{
			"type":    "text",
			"content": t.Preview.Text,
			"state":   agui.PartStateStreaming,
		}}
	} else {
		message.Parts = []agui.MessagePart{}
	}
	return message
}

func (t Run) UIMessageMetadata(includeUsage bool) UIMessageMetadata {
	metadata := UIMessageMetadata{
		ThreadID: t.ThreadID,
		RunID:    t.RunID,
		Status:   t.Status,
	}
	if includeUsage {
		metadata.Usage = &t.Usage
	}
	return metadata
}

func compactTextPart(part agui.MessagePart, budget int) {
	if part == nil {
		return
	}
	content, _ := part["content"].(string)
	if budget <= 0 {
		if part["state"] == "" {
			part["state"] = agui.PartStateDone
		}
		return
	}
	preview := BoundedPreview(content, budget)
	part["content"] = preview
	if len(preview) < len(content) {
		part["providerMetadata"] = map[string]any{"truncated": true}
	}
	if part["state"] == "" {
		part["state"] = agui.PartStateDone
	}
}

func asString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	case nil:
		return ""
	default:
		return fmt.Sprint(typed)
	}
}

func firstString(values ...any) string {
	for _, value := range values {
		if text, ok := value.(string); ok && text != "" {
			return text
		}
	}
	return ""
}

func approvalMapID(value any) string {
	switch typed := value.(type) {
	case agui.ToolApproval:
		return typed.ID
	case *agui.ToolApproval:
		if typed != nil {
			return typed.ID
		}
	case agui.ToolApprovalResponse:
		return typed.ID
	case *agui.ToolApprovalResponse:
		if typed != nil {
			return typed.ID
		}
	case map[string]any:
		id, _ := typed["id"].(string)
		return id
	}
	return ""
}

func (t Run) Metadata() map[string]any {
	return t.RunMetadata().Map()
}

func (t Run) RunMetadata() RunMetadata {
	return RunMetadata{
		Schema:    "com.beeper.ai.run.v1",
		Protocol:  "ag-ui",
		ThreadID:  t.ThreadID,
		RunID:     t.RunID,
		MessageID: t.MessageID,
		AgentID:   t.AgentID,
		AgentName: t.AgentName,
		Model:     t.Model,
		Usage:     t.Usage,
		Status:    t.Status,
		Approvals: t.Approvals,
		Artifacts: t.Artifacts,
		Data:      t.Data,
		Preview:   t.Preview,
	}
}

func (t Run) Validate() error {
	for i, evt := range t.Events {
		if err := agui.ValidateEvent(evt); err != nil {
			return fmt.Errorf("event %d: %w", i+1, err)
		}
	}
	return nil
}

func PreviewFromText(text string, budget int) Preview {
	preview := BoundedPreview(text, budget)
	return Preview{Text: preview, Truncated: len(preview) < len(text)}
}

func BoundedPreview(text string, budget int) string {
	text = strings.TrimSpace(text)
	if budget <= 0 || len(text) <= budget {
		return text
	}
	end := budget
	for end > 0 && !utf8.RuneStart(text[end]) {
		end--
	}
	if end <= 0 {
		return ""
	}
	return strings.TrimSpace(text[:end])
}

func SplitTextUTF8(text string, maxBytes int) []string {
	if maxBytes <= 0 {
		return nil
	}
	if len(text) <= maxBytes {
		return []string{text}
	}
	var chunks []string
	start := 0
	for start < len(text) {
		end := start + maxBytes
		if end >= len(text) {
			chunks = append(chunks, text[start:])
			break
		}
		for end > start && !utf8.RuneStart(text[end]) {
			end--
		}
		if end == start {
			_, size := utf8.DecodeRuneInString(text[start:])
			end = start + size
		}
		chunks = append(chunks, text[start:end])
		start = end
	}
	return chunks
}

func JSONSize(value any) int {
	raw, err := json.Marshal(value)
	if err != nil {
		return CarrierBudgetBytes + 1
	}
	return len(raw)
}
