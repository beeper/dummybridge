package agui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	EventRunStarted         = "RUN_STARTED"
	EventRunFinished        = "RUN_FINISHED"
	EventRunError           = "RUN_ERROR"
	EventTextMessageStart   = "TEXT_MESSAGE_START"
	EventTextMessageContent = "TEXT_MESSAGE_CONTENT"
	EventTextMessageEnd     = "TEXT_MESSAGE_END"
	EventToolCallStart      = "TOOL_CALL_START"
	EventToolCallArgs       = "TOOL_CALL_ARGS"
	EventToolCallEnd        = "TOOL_CALL_END"
	EventToolCallResult     = "TOOL_CALL_RESULT"
	EventStepStarted        = "STEP_STARTED"
	EventStepFinished       = "STEP_FINISHED"
	EventStateSnapshot      = "STATE_SNAPSHOT"
	EventStateDelta         = "STATE_DELTA"
	EventMessagesSnapshot   = "MESSAGES_SNAPSHOT"
	EventCustom             = "CUSTOM"
	EventReasoningStart     = "REASONING_START"
	EventReasoningEnd       = "REASONING_END"
	EventReasoningMsgStart  = "REASONING_MESSAGE_START"
	EventReasoningMsgCont   = "REASONING_MESSAGE_CONTENT"
	EventReasoningMsgEnd    = "REASONING_MESSAGE_END"
)

const (
	RoleAssistant = "assistant"
	RoleUser      = "user"
	RoleSystem    = "system"
	RoleTool      = "tool"
)

const (
	ToolStateAwaitingInput     = "awaiting-input"
	ToolStateInputStreaming    = "input-streaming"
	ToolStateInputComplete     = "input-complete"
	ToolStateApprovalRequested = "approval-requested"
	ToolStateApprovalResponded = "approval-responded"
	ToolResultStateStreaming   = "streaming"
	ToolResultStateComplete    = "complete"
	ToolResultStateError       = "error"
	PartStateStreaming         = "streaming"
	PartStateDone              = "done"
	ApprovalCustomRequested    = "approval-requested"
	ApprovalCustomResponded    = "approval-responded"
	FinishReasonStop           = "stop"
	FinishReasonLength         = "length"
	FinishReasonContentFilter  = "content_filter"
	FinishReasonToolCalls      = "tool_calls"
	FinishReasonOther          = "other"
)

type Event map[string]any

type UIMessage struct {
	ID        string         `json:"id"`
	Role      string         `json:"role"`
	Parts     []MessagePart  `json:"parts"`
	CreatedAt *time.Time     `json:"createdAt,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type MessagePart map[string]any

type RunAgentInput struct {
	ThreadID       string         `json:"threadId,omitempty"`
	RunID          string         `json:"runId,omitempty"`
	State          map[string]any `json:"state,omitempty"`
	Messages       []UIMessage    `json:"messages,omitempty"`
	Tools          []Tool         `json:"tools,omitempty"`
	Context        []ContextItem  `json:"context,omitempty"`
	ForwardedProps map[string]any `json:"forwardedProps,omitempty"`
	Data           map[string]any `json:"data,omitempty"`
}

type Tool struct {
	Name          string         `json:"name"`
	Description   string         `json:"description,omitempty"`
	InputSchema   map[string]any `json:"inputSchema,omitempty"`
	OutputSchema  map[string]any `json:"outputSchema,omitempty"`
	NeedsApproval bool           `json:"needsApproval,omitempty"`
}

type ContextItem struct {
	Type  string         `json:"type"`
	Value any            `json:"value,omitempty"`
	Meta  map[string]any `json:"meta,omitempty"`
}

type ToolApproval struct {
	ID            string         `json:"id"`
	NeedsApproval bool           `json:"needsApproval"`
	Fields        map[string]any `json:"fields,omitempty"`
}

type ToolApprovalResponse struct {
	ID       string         `json:"id"`
	Approved bool           `json:"approved"`
	Always   bool           `json:"always,omitempty"`
	Reason   string         `json:"reason,omitempty"`
	Fields   map[string]any `json:"fields,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type Usage struct {
	PromptTokens     int `json:"promptTokens,omitempty"`
	CompletionTokens int `json:"completionTokens,omitempty"`
	TotalTokens      int `json:"totalTokens,omitempty"`
}

type EventBuilder struct {
	now   func() time.Time
	model string
}

func NewEventBuilder(model string, now func() time.Time) EventBuilder {
	if now == nil {
		now = time.Now
	}
	return EventBuilder{now: now, model: strings.TrimSpace(model)}
}

func (b EventBuilder) base(eventType string) Event {
	evt := Event{
		"type":      eventType,
		"timestamp": b.now().UnixMilli(),
	}
	if b.model != "" {
		evt["model"] = b.model
	}
	return evt
}

func (b EventBuilder) RunStarted(threadID, runID string) Event {
	evt := b.base(EventRunStarted)
	evt["threadId"] = threadID
	evt["runId"] = runID
	return evt
}

func (b EventBuilder) RunFinished(threadID, runID, finishReason string, usage Usage) Event {
	evt := b.base(EventRunFinished)
	evt["threadId"] = threadID
	evt["runId"] = runID
	evt["finishReason"] = NormalizeFinishReason(finishReason)
	evt["usage"] = usage
	return evt
}

func (b EventBuilder) RunError(threadID, runID, message string) Event {
	evt := b.base(EventRunError)
	evt["threadId"] = threadID
	if strings.TrimSpace(runID) != "" {
		evt["runId"] = runID
	}
	evt["message"] = message
	evt["error"] = map[string]any{"message": message}
	return evt
}

func (b EventBuilder) TextMessageStart(messageID, role string) Event {
	if role == "" {
		role = RoleAssistant
	}
	evt := b.base(EventTextMessageStart)
	evt["messageId"] = messageID
	evt["role"] = role
	return evt
}

func (b EventBuilder) TextMessageContent(messageID, delta string) Event {
	evt := b.base(EventTextMessageContent)
	evt["messageId"] = messageID
	evt["delta"] = delta
	return evt
}

func (b EventBuilder) TextMessageEnd(messageID string) Event {
	evt := b.base(EventTextMessageEnd)
	evt["messageId"] = messageID
	return evt
}

func (b EventBuilder) ReasoningStart(messageID string) Event {
	evt := b.base(EventReasoningStart)
	evt["messageId"] = messageID
	return evt
}

func (b EventBuilder) ReasoningEnd(messageID string) Event {
	evt := b.base(EventReasoningEnd)
	evt["messageId"] = messageID
	return evt
}

func (b EventBuilder) ReasoningMessageStart(messageID string) Event {
	evt := b.base(EventReasoningMsgStart)
	evt["messageId"] = messageID
	return evt
}

func (b EventBuilder) ReasoningMessageContent(messageID, delta string) Event {
	evt := b.base(EventReasoningMsgCont)
	evt["messageId"] = messageID
	evt["delta"] = delta
	return evt
}

func (b EventBuilder) ReasoningMessageEnd(messageID string) Event {
	evt := b.base(EventReasoningMsgEnd)
	evt["messageId"] = messageID
	return evt
}

func (b EventBuilder) ToolCallStart(messageID, toolCallID, name string, index *int, approval *ToolApproval) Event {
	return b.ToolCallStartWithMetadata(messageID, toolCallID, name, index, approval, nil)
}

func (b EventBuilder) ToolCallStartWithMetadata(messageID, toolCallID, name string, index *int, approval *ToolApproval, metadata map[string]any) Event {
	evt := b.base(EventToolCallStart)
	if messageID != "" {
		evt["parentMessageId"] = messageID
	}
	evt["toolCallId"] = toolCallID
	evt["toolCallName"] = name
	evt["toolName"] = name
	if len(metadata) > 0 {
		evt["metadata"] = metadata
	}
	if index != nil {
		evt["index"] = *index
	}
	if approval != nil {
		evt["approval"] = approval
		evt["state"] = ToolStateApprovalRequested
	} else {
		evt["state"] = ToolStateAwaitingInput
	}
	return evt
}

func (b EventBuilder) ToolCallArgs(toolCallID, delta string, args any) Event {
	evt := b.base(EventToolCallArgs)
	evt["toolCallId"] = toolCallID
	evt["delta"] = delta
	evt["state"] = ToolStateInputStreaming
	if args != nil {
		evt["args"] = args
	}
	return evt
}

func (b EventBuilder) ToolCallEnd(toolCallID, name string, input, result any, state string) Event {
	evt := b.base(EventToolCallEnd)
	evt["toolCallId"] = toolCallID
	evt["toolCallName"] = name
	evt["toolName"] = name
	if input != nil {
		evt["input"] = input
	}
	if result != nil {
		evt["result"] = result
	}
	if state == "" {
		state = ToolStateInputComplete
	}
	evt["state"] = state
	return evt
}

func (b EventBuilder) ToolCallResult(messageID, toolCallID, content, state, role string) Event {
	if role == "" {
		role = RoleTool
	}
	if state == "" {
		state = ToolResultStateComplete
	}
	evt := b.base(EventToolCallResult)
	evt["messageId"] = messageID
	evt["toolCallId"] = toolCallID
	evt["content"] = content
	evt["state"] = state
	evt["role"] = role
	return evt
}

func (b EventBuilder) StepStarted(messageID, stepName string) Event {
	if stepName == "" {
		panic("ag-ui: stepName is required for STEP_STARTED")
	}
	evt := b.base(EventStepStarted)
	if messageID != "" {
		evt["messageId"] = messageID
	}
	evt["stepName"] = stepName
	return evt
}

func (b EventBuilder) StepFinished(messageID, stepName string) Event {
	if stepName == "" {
		panic("ag-ui: stepName is required for STEP_FINISHED")
	}
	evt := b.base(EventStepFinished)
	if messageID != "" {
		evt["messageId"] = messageID
	}
	evt["stepName"] = stepName
	return evt
}

func (b EventBuilder) StateSnapshot(state map[string]any) Event {
	evt := b.base(EventStateSnapshot)
	evt["snapshot"] = state
	return evt
}

func (b EventBuilder) StateDelta(delta any) Event {
	evt := b.base(EventStateDelta)
	evt["delta"] = delta
	return evt
}

func (b EventBuilder) MessagesSnapshot(messages []UIMessage) Event {
	evt := b.base(EventMessagesSnapshot)
	evt["messages"] = messages
	return evt
}

func (b EventBuilder) Custom(name string, value any) Event {
	evt := b.base(EventCustom)
	evt["name"] = name
	evt["value"] = value
	return evt
}

func TextPart(content string) MessagePart {
	return MessagePart{"type": "text", "content": content}
}

func ThinkingPart(content string) MessagePart {
	return MessagePart{"type": "thinking", "content": content}
}

func ToolCallPart(id, name string, arguments any, state string, approval *ToolApproval, output any) MessagePart {
	part := MessagePart{"type": "tool-call", "id": id, "name": name, "arguments": arguments, "state": state}
	if approval != nil {
		part["approval"] = approval
	}
	if output != nil {
		part["output"] = output
	}
	return part
}

func ToolResultPart(toolCallID string, content any, state string, err any) MessagePart {
	part := MessagePart{"type": "tool-result", "toolCallId": toolCallID, "content": content, "state": state}
	if err != nil {
		part["error"] = err
	}
	return part
}

func ValidateEvent(evt Event) error {
	eventType, _ := evt["type"].(string)
	if eventType == "" {
		return fmt.Errorf("ag-ui event missing type")
	}
	if _, ok := evt["timestamp"]; !ok {
		return fmt.Errorf("%s missing timestamp", eventType)
	}
	switch eventType {
	case EventRunStarted:
		return require(evt, "threadId", "runId")
	case EventRunFinished:
		return require(evt, "threadId", "runId", "finishReason")
	case EventRunError:
		return require(evt, "message")
	case EventTextMessageStart:
		return require(evt, "messageId", "role")
	case EventTextMessageContent:
		if err := require(evt, "messageId"); err != nil {
			return err
		}
		return requireStringField(evt, "delta")
	case EventTextMessageEnd:
		return require(evt, "messageId")
	case EventReasoningStart, EventReasoningEnd, EventReasoningMsgStart, EventReasoningMsgEnd:
		return require(evt, "messageId")
	case EventReasoningMsgCont:
		if err := require(evt, "messageId"); err != nil {
			return err
		}
		return requireStringField(evt, "delta")
	case EventToolCallStart:
		if err := require(evt, "toolCallId", "toolCallName"); err != nil {
			return err
		}
		if approval, ok := evt["approval"]; ok {
			if err := validateToolApproval(approval); err != nil {
				return fmt.Errorf("%s has invalid approval: %w", evt["type"], err)
			}
		}
		return validateStringSet(evt, "state", true, validToolStates)
	case EventToolCallArgs:
		if err := require(evt, "toolCallId"); err != nil {
			return err
		}
		if err := requireStringField(evt, "delta"); err != nil {
			return err
		}
		if err := validateStringSet(evt, "state", false, validToolStates); err != nil {
			return err
		}
		if args, ok := evt["args"]; ok {
			if _, ok := args.(string); !ok {
				return fmt.Errorf("%s has invalid args %T", evt["type"], args)
			}
		}
		return nil
	case EventToolCallEnd:
		if err := require(evt, "toolCallId"); err != nil {
			return err
		}
		if result, ok := evt["result"]; ok {
			if _, ok := result.(string); !ok {
				return fmt.Errorf("%s has invalid result %T", evt["type"], result)
			}
		}
		return validateStringSet(evt, "state", true, validToolStates)
	case EventToolCallResult:
		if err := require(evt, "messageId", "toolCallId", "content"); err != nil {
			return err
		}
		return validateStringSet(evt, "state", false, validToolResultStates)
	case EventStepStarted, EventStepFinished:
		return require(evt, "stepName")
	case EventStateSnapshot:
		return require(evt, "snapshot")
	case EventStateDelta:
		return require(evt, "delta")
	case EventMessagesSnapshot:
		return require(evt, "messages")
	case EventCustom:
		return require(evt, "name")
	default:
		return fmt.Errorf("unsupported ag-ui event type %q", eventType)
	}
}

func validateToolApproval(value any) error {
	switch approval := value.(type) {
	case ToolApproval:
		if strings.TrimSpace(approval.ID) == "" {
			return fmt.Errorf("missing id")
		}
		if !approval.NeedsApproval {
			return fmt.Errorf("needsApproval must be true")
		}
		return nil
	case *ToolApproval:
		if approval == nil {
			return fmt.Errorf("missing approval")
		}
		return validateToolApproval(*approval)
	case map[string]any:
		id, _ := approval["id"].(string)
		if strings.TrimSpace(id) == "" {
			return fmt.Errorf("missing id")
		}
		if approval["needsApproval"] != true {
			return fmt.Errorf("needsApproval must be true")
		}
		return nil
	default:
		return fmt.Errorf("unexpected %T", value)
	}
}

func ValidateEventSequence(events []Event) error {
	seenRunStart := false
	terminal := false
	textOpen := map[string]bool{}
	reasoningOpen := map[string]bool{}
	toolStarted := map[string]bool{}
	toolEnded := map[string]bool{}

	for i, evt := range events {
		if err := ValidateEvent(evt); err != nil {
			return fmt.Errorf("event %d: %w", i+1, err)
		}
		eventType, _ := evt["type"].(string)
		if terminal {
			return fmt.Errorf("event %d: %s after terminal run event", i+1, eventType)
		}

		switch eventType {
		case EventRunStarted:
			if seenRunStart {
				return fmt.Errorf("event %d: duplicate RUN_STARTED", i+1)
			}
			seenRunStart = true
		case EventRunFinished:
			if !seenRunStart {
				return fmt.Errorf("event %d: RUN_FINISHED before RUN_STARTED", i+1)
			}
			terminal = true
		case EventRunError:
			terminal = true
		case EventTextMessageStart:
			messageID := stringField(evt, "messageId")
			if textOpen[messageID] {
				return fmt.Errorf("event %d: duplicate TEXT_MESSAGE_START for %s", i+1, messageID)
			}
			textOpen[messageID] = true
		case EventTextMessageContent:
			messageID := stringField(evt, "messageId")
			if !textOpen[messageID] {
				return fmt.Errorf("event %d: TEXT_MESSAGE_CONTENT before TEXT_MESSAGE_START for %s", i+1, messageID)
			}
		case EventTextMessageEnd:
			messageID := stringField(evt, "messageId")
			if !textOpen[messageID] {
				return fmt.Errorf("event %d: TEXT_MESSAGE_END before TEXT_MESSAGE_START for %s", i+1, messageID)
			}
			delete(textOpen, messageID)
		case EventReasoningMsgStart:
			messageID := stringField(evt, "messageId")
			if reasoningOpen[messageID] {
				return fmt.Errorf("event %d: duplicate REASONING_MESSAGE_START for %s", i+1, messageID)
			}
			reasoningOpen[messageID] = true
		case EventReasoningMsgCont:
			messageID := stringField(evt, "messageId")
			if !reasoningOpen[messageID] {
				return fmt.Errorf("event %d: REASONING_MESSAGE_CONTENT before REASONING_MESSAGE_START for %s", i+1, messageID)
			}
		case EventReasoningMsgEnd:
			messageID := stringField(evt, "messageId")
			if !reasoningOpen[messageID] {
				return fmt.Errorf("event %d: REASONING_MESSAGE_END before REASONING_MESSAGE_START for %s", i+1, messageID)
			}
			delete(reasoningOpen, messageID)
		case EventToolCallStart:
			toolCallID := stringField(evt, "toolCallId")
			if toolStarted[toolCallID] {
				return fmt.Errorf("event %d: duplicate TOOL_CALL_START for %s", i+1, toolCallID)
			}
			toolStarted[toolCallID] = true
		case EventToolCallArgs:
			toolCallID := stringField(evt, "toolCallId")
			if !toolStarted[toolCallID] {
				return fmt.Errorf("event %d: TOOL_CALL_ARGS before TOOL_CALL_START for %s", i+1, toolCallID)
			}
		case EventToolCallEnd:
			toolCallID := stringField(evt, "toolCallId")
			if !toolStarted[toolCallID] {
				return fmt.Errorf("event %d: TOOL_CALL_END before TOOL_CALL_START for %s", i+1, toolCallID)
			}
			if toolEnded[toolCallID] {
				return fmt.Errorf("event %d: duplicate TOOL_CALL_END for %s", i+1, toolCallID)
			}
			toolEnded[toolCallID] = true
		case EventToolCallResult:
			toolCallID := stringField(evt, "toolCallId")
			if !toolStarted[toolCallID] {
				return fmt.Errorf("event %d: TOOL_CALL_RESULT before TOOL_CALL_START for %s", i+1, toolCallID)
			}
		}
	}
	return nil
}

var validToolStates = map[string]bool{
	ToolStateAwaitingInput:     true,
	ToolStateInputStreaming:    true,
	ToolStateInputComplete:     true,
	ToolStateApprovalRequested: true,
	ToolStateApprovalResponded: true,
}

func stringField(evt Event, key string) string {
	value, _ := evt[key].(string)
	return value
}

var validToolResultStates = map[string]bool{
	ToolResultStateStreaming: true,
	ToolResultStateComplete:  true,
	ToolResultStateError:     true,
}

func validateStringSet(evt Event, key string, required bool, allowed map[string]bool) error {
	value, ok := evt[key]
	if !ok || value == nil {
		if required {
			return fmt.Errorf("%s missing %s", evt["type"], key)
		}
		return nil
	}
	stringValue, ok := value.(string)
	if !ok || !allowed[stringValue] {
		return fmt.Errorf("%s has invalid %s %q", evt["type"], key, value)
	}
	return nil
}

func NormalizeFinishReason(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "", FinishReasonStop:
		return FinishReasonStop
	case FinishReasonLength:
		return FinishReasonLength
	case "content-filter", "contentfilter", FinishReasonContentFilter:
		return FinishReasonContentFilter
	case "tool-calls", "toolcalls", FinishReasonToolCalls:
		return FinishReasonToolCalls
	case FinishReasonOther:
		return FinishReasonOther
	default:
		return FinishReasonStop
	}
}

func CloneEvent(evt Event) Event {
	raw, err := json.Marshal(evt)
	if err != nil {
		cp := make(Event, len(evt))
		for k, v := range evt {
			cp[k] = v
		}
		return cp
	}
	var cp Event
	if err := json.Unmarshal(raw, &cp); err != nil {
		cp = make(Event, len(evt))
		for k, v := range evt {
			cp[k] = v
		}
	}
	return cp
}

func require(evt Event, keys ...string) error {
	for _, key := range keys {
		value, ok := evt[key]
		if !ok || emptyValue(value) {
			return fmt.Errorf("%s missing %s", evt["type"], key)
		}
	}
	return nil
}

// requireStringField checks that the field is present and is a string.
// Unlike require, it accepts whitespace-only strings — streaming deltas can
// legitimately consist only of spaces or newlines between tokens.
func requireStringField(evt Event, key string) error {
	value, ok := evt[key]
	if !ok {
		return fmt.Errorf("%s missing %s", evt["type"], key)
	}
	str, ok := value.(string)
	if !ok {
		return fmt.Errorf("%s has invalid %s %T", evt["type"], key, value)
	}
	if str == "" {
		return fmt.Errorf("%s missing %s", evt["type"], key)
	}
	return nil
}

func emptyValue(value any) bool {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v) == ""
	case nil:
		return true
	default:
		return false
	}
}
