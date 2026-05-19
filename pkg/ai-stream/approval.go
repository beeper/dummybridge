package aistream

import (
	"strings"
	"time"

	"github.com/beeper/dummybridge/pkg/ag-ui"
)

const (
	ApprovalReactionAllowOnce   = "approval.allow_once"
	ApprovalReactionAllowAlways = "approval.allow_always"
	ApprovalReactionDeny        = "approval.deny"
)

type ReactionOption[T any] struct {
	ID     string   `json:"id"`
	Label  string   `json:"label"`
	Values []string `json:"values"`
	Value  T        `json:"value"`
}

type ApprovalCleanup[T any] struct {
	Selected              ReactionOption[T]
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
	ID          string `json:"id"`
	ThreadID    string `json:"threadId"`
	RunID       string `json:"runId"`
	MessageID   string `json:"messageId"`
	ToolCallID  string `json:"toolCallId"`
	ToolName    string `json:"toolName"`
	TargetEvent string `json:"target_event"`
	AgentID     string `json:"agentId,omitempty"`
	AgentName   string `json:"agentName,omitempty"`
	Model       string `json:"model,omitempty"`
	SeqStart    int    `json:"seqStart,omitempty"`
}

func DefaultApprovalOptions(approvalID string) []ReactionOption[agui.ToolApprovalResponse] {
	return []ReactionOption[agui.ToolApprovalResponse]{
		{
			ID:     ApprovalReactionAllowOnce,
			Label:  "Allow",
			Values: []string{"👍", "approval.allow_once", "allow", "allow_once"},
			Value:  agui.ToolApprovalResponse{ID: approvalID, Approved: true},
		},
		{
			ID:     ApprovalReactionAllowAlways,
			Label:  "Always allow",
			Values: []string{"✅", "approval.allow_always", "always", "allow_always"},
			Value:  agui.ToolApprovalResponse{ID: approvalID, Approved: true, Always: true},
		},
		{
			ID:     ApprovalReactionDeny,
			Label:  "Deny",
			Values: []string{"👎", "approval.deny", "deny", "reject"},
			Value:  agui.ToolApprovalResponse{ID: approvalID, Approved: false, Reason: "denied"},
		},
	}
}

func ResolveReaction[T any](options []ReactionOption[T], raw string) (ReactionOption[T], bool) {
	key := NormalizeReaction(raw)
	for _, option := range options {
		if NormalizeReaction(option.ID) == key {
			return option, true
		}
		for _, value := range option.Values {
			if NormalizeReaction(value) == key {
				return option, true
			}
		}
	}
	var zero ReactionOption[T]
	return zero, false
}

func CleanupReactions[T any](options []ReactionOption[T], selectedKey string, events []ReactionEvent, bridgeSender string) ApprovalCleanup[T] {
	selected, ok := ResolveReaction(options, selectedKey)
	if !ok {
		return ApprovalCleanup[T]{}
	}
	cleanup := ApprovalCleanup[T]{Selected: selected, Matched: true}
	for _, evt := range events {
		if evt.EventID == "" {
			continue
		}
		option, matchesOption := ResolveReaction(options, evt.Key)
		isSelected := matchesOption && option.ID == selected.ID
		isBridge := evt.Bridge || (bridgeSender != "" && evt.Sender == bridgeSender)
		if isSelected && !isBridge && cleanup.SelectedReactionEvent == "" {
			cleanup.SelectedReactionEvent = evt.EventID
			continue
		}
		if isBridge || (matchesOption && !isSelected) {
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

func ApprovalResponseRun(ctx ApprovalContext, response agui.ToolApprovalResponse, now time.Time) Run {
	if response.ID == "" {
		response.ID = ctx.ID
	}
	agentID := ctx.AgentID
	if agentID == "" {
		agentID = "ai"
	}
	agentName := ctx.AgentName
	if agentName == "" {
		agentName = "AI"
	}
	model := ctx.Model
	if model == "" {
		model = DefaultModel
	}
	run := NewRun("approval-"+ctx.ID, ctx.ThreadID, model, agentID, agentName, now)
	run.RunID = ctx.RunID
	run.MessageID = ctx.MessageID
	run.ToolCallID = ctx.ToolCallID
	run.ApprovalID = ctx.ID
	run.Status = Status{State: "complete"}
	run.Approvals = []ApprovalSummary{{
		ID:         ctx.ID,
		ToolCallID: ctx.ToolCallID,
		State:      approvalSummaryState(response),
		Always:     response.Always,
		Reason:     response.Reason,
		Fields:     response.Fields,
		Metadata:   response.Metadata,
	}}
	builder := agui.NewEventBuilder(model, func() time.Time { return now })
	run.Events = append(run.Events, builder.Custom(agui.ApprovalCustomResponded, map[string]any{
		"threadId":   ctx.ThreadID,
		"runId":      ctx.RunID,
		"messageId":  ctx.MessageID,
		"toolCallId": ctx.ToolCallID,
		"toolName":   ctx.ToolName,
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
		result["approved"] = true
	} else {
		reason := response.Reason
		if reason == "" {
			reason = "denied"
		}
		result["state"] = agui.ToolResultStateError
		result["reason"] = reason
		run.Status = Status{State: "error", Error: result}
	}
	run.Events = append(run.Events, builder.ToolCallEnd(ctx.ToolCallID, ctx.ToolName, nil, jsonString(result), agui.ToolStateApprovalResponded))
	return *run
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
