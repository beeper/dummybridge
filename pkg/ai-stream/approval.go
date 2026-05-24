package aistream

import (
	"strings"

	"github.com/beeper/dummybridge/pkg/ag-ui"
)

const (
	ApprovalChoiceApprove       = "approve"
	ApprovalChoiceAlwaysApprove = "always_approve"
	ApprovalChoiceDeny          = "deny"
)

type ApprovalChoice struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Alias    string `json:"alias"`
	Style    string `json:"style,omitempty"`
	Shortcut string `json:"shortcut,omitempty"`
}

type ApprovalCleanup struct {
	Selected              ApprovalChoice
	SelectedReactionEvent string
	RedactReactionEvents  []string
	Matched               bool
}

type ReactionEvent struct {
	EventID string
	Sender  string
	Key     string
	Bridge  bool
}

type ApprovalContext struct {
	ID               string `json:"id"`
	ThreadID         string `json:"threadId"`
	RunID            string `json:"runId"`
	MessageID        string `json:"messageId"`
	Command          string `json:"command"`
	ToolCallID       string `json:"toolCallId"`
	ToolName         string `json:"toolName"`
	TargetEvent      string `json:"target_event"`
	AgentID          string `json:"agentId,omitempty"`
	AgentName        string `json:"agentName,omitempty"`
	Model            string `json:"model,omitempty"`
	SeqStart         int    `json:"seqStart,omitempty"`
	PreviewText      string `json:"previewText,omitempty"`
	PreviewTruncated bool   `json:"previewTruncated,omitempty"`
}

type ApprovalRequestedValue struct {
	ThreadID          string
	RunID             string
	MessageID         string
	ToolCallID        string
	ToolName          string
	Input             any
	Approval          agui.ToolApproval
	ApprovalMessageID string
	ApprovalEventID   string
	Choices           []ApprovalChoice
	Metadata          map[string]any
}

type ApprovalNotice struct {
	Schema     string
	ID         string
	MessageID  string
	ToolCallID string
	ToolName   string
	State      string
	Choices    []ApprovalChoice
}

func NewApprovalRequestedValue(run Run, toolCallID, toolName string, input any, approval agui.ToolApproval) ApprovalRequestedValue {
	return ApprovalRequestedValue{
		ThreadID:          run.ThreadID,
		RunID:             run.RunID,
		MessageID:         run.MessageID,
		ToolCallID:        toolCallID,
		ToolName:          toolName,
		Input:             input,
		Approval:          approval,
		ApprovalMessageID: approval.ID,
		Choices:           DefaultApprovalChoices(),
	}
}

func NewApprovalNotice(ctx ApprovalContext, choices []ApprovalChoice) ApprovalNotice {
	return ApprovalNotice{
		Schema:     "com.beeper.ai.approval.v1",
		ID:         ctx.ID,
		MessageID:  ctx.MessageID,
		ToolCallID: ctx.ToolCallID,
		ToolName:   ctx.ToolName,
		State:      "requested",
		Choices:    choices,
	}
}

func (v ApprovalRequestedValue) Map() map[string]any {
	value := map[string]any{
		"threadId":          v.ThreadID,
		"runId":             v.RunID,
		"messageId":         v.MessageID,
		"toolCallId":        v.ToolCallID,
		"toolName":          v.ToolName,
		"input":             v.Input,
		"approval":          v.Approval,
		"approvalMessageId": v.ApprovalMessageID,
		"choices":           v.Choices,
	}
	if v.ApprovalEventID != "" {
		value["approvalEventId"] = v.ApprovalEventID
	}
	if len(v.Metadata) > 0 {
		value["metadata"] = v.Metadata
	}
	return value
}

func (n ApprovalNotice) Map() map[string]any {
	return map[string]any{
		"schema":     n.Schema,
		"id":         n.ID,
		"messageId":  n.MessageID,
		"toolCallId": n.ToolCallID,
		"toolName":   n.ToolName,
		"state":      n.State,
		"choices":    ApprovalChoicesAsAny(n.Choices),
	}
}

func ApprovalChoicesAsAny(choices []ApprovalChoice) []any {
	out := make([]any, 0, len(choices))
	for _, choice := range choices {
		item := map[string]any{
			"key":   choice.Key,
			"label": choice.Label,
			"alias": choice.Alias,
		}
		if choice.Style != "" {
			item["style"] = choice.Style
		}
		if choice.Shortcut != "" {
			item["shortcut"] = choice.Shortcut
		}
		out = append(out, item)
	}
	return out
}

func ApprovalIDFromRequestedValue(value map[string]any) string {
	approval, _ := value["approval"].(agui.ToolApproval)
	if approval.ID != "" {
		return approval.ID
	}
	if raw, ok := value["approval"].(map[string]any); ok {
		approvalID, _ := raw["id"].(string)
		return approvalID
	}
	return ""
}

func SetApprovalRequestedEventID(value map[string]any, eventID string) bool {
	if value == nil || eventID == "" {
		return false
	}
	approvalID := ApprovalIDFromRequestedValue(value)
	if approvalID == "" {
		return false
	}
	value["approvalMessageId"] = approvalID
	value["approvalEventId"] = eventID
	return true
}

func DefaultApprovalChoices() []ApprovalChoice {
	return []ApprovalChoice{
		{
			Key:   ApprovalChoiceApprove,
			Label: "Allow once",
			Alias: "✅",
		},
		{
			Key:   ApprovalChoiceAlwaysApprove,
			Label: "Allow always",
			Alias: "☑️",
		},
		{
			Key:   ApprovalChoiceDeny,
			Label: "Deny",
			Alias: "❌",
			Style: "danger",
		},
	}
}

func ResolveApprovalChoice(choices []ApprovalChoice, raw string) (ApprovalChoice, bool) {
	key := NormalizeReaction(raw)
	for _, choice := range choices {
		if NormalizeReaction(choice.Key) == key || NormalizeReaction(choice.Alias) == key {
			return choice, true
		}
	}
	var zero ApprovalChoice
	return zero, false
}

func ApprovalResponseForChoice(approvalID string, choice ApprovalChoice) agui.ToolApprovalResponse {
	switch choice.Key {
	case ApprovalChoiceApprove:
		return agui.ToolApprovalResponse{ID: approvalID, Approved: true}
	case ApprovalChoiceAlwaysApprove:
		return agui.ToolApprovalResponse{ID: approvalID, Approved: true, Always: true}
	case ApprovalChoiceDeny:
		return agui.ToolApprovalResponse{ID: approvalID, Approved: false, Reason: "denied"}
	default:
		return agui.ToolApprovalResponse{ID: approvalID, Approved: false, Reason: "invalid approval choice"}
	}
}

func CleanupApprovalReactions(choices []ApprovalChoice, selectedKey string, events []ReactionEvent, bridgeSender string) ApprovalCleanup {
	selected, ok := ResolveApprovalChoice(choices, selectedKey)
	if !ok {
		return ApprovalCleanup{}
	}
	cleanup := ApprovalCleanup{Selected: selected, Matched: true}
	for _, evt := range events {
		if evt.EventID == "" {
			continue
		}
		choice, matchesChoice := ResolveApprovalChoice(choices, evt.Key)
		isSelected := matchesChoice && choice.Key == selected.Key
		isBridge := evt.Bridge || (bridgeSender != "" && evt.Sender == bridgeSender)
		if isSelected && !isBridge && cleanup.SelectedReactionEvent == "" {
			cleanup.SelectedReactionEvent = evt.EventID
			continue
		}
		if isBridge || (matchesChoice && !isSelected) {
			cleanup.RedactReactionEvents = append(cleanup.RedactReactionEvents, evt.EventID)
		}
	}
	return cleanup
}

func NormalizeReaction(reaction string) string {
	reaction = strings.TrimSpace(reaction)
	reaction = strings.ReplaceAll(reaction, "\ufe0f", "")
	return strings.ToLower(reaction)
}

func approvalSummaryState(response agui.ToolApprovalResponse) string {
	if response.Approved {
		if response.Always {
			return "approved-always"
		}
		return "approved"
	}
	return "denied"
}
