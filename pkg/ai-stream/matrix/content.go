package matrix

import (
	"fmt"

	"github.com/beeper/dummybridge/pkg/ag-ui"
	"github.com/beeper/dummybridge/pkg/ai-stream"
	"maunium.net/go/mautrix/format"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

func AnchorContent(run aistream.Run) (*event.MessageEventContent, map[string]any) {
	body := run.Preview.Text
	if body == "" {
		body = "..."
	}
	rendered := format.RenderMarkdown(body, true, false)
	content := &rendered
	content.BeeperPerMessageProfile = &event.BeeperPerMessageProfile{
		ID:          run.AgentID,
		Displayname: run.AgentName,
	}
	extra := map[string]any{
		aistream.BeeperAIKey:         run.InitialUIMessage(),
		aistream.BeeperAIMetadataKey: run.Metadata(),
		"com.beeper.stream": map[string]any{
			"type": aistream.BeeperAIStreamDeltas,
		},
	}
	return content, extra
}

func CarrierContent(carrier aistream.Carrier, targetEventID id.EventID) (*event.MessageEventContent, map[string]any) {
	content := &event.MessageEventContent{
		MsgType:  event.MsgText,
		Body:     "",
		Mentions: &event.Mentions{},
		RelatesTo: &event.RelatesTo{
			Type:    event.RelReference,
			EventID: targetEventID,
		},
	}
	return content, aistream.CarrierContent(carrier.Envelopes)
}

func ApprovalContent(ctx aistream.ApprovalContext, options []aistream.ReactionOption[agui.ToolApprovalResponse]) (*event.MessageEventContent, map[string]any) {
	toolName := ctx.ToolName
	body := fmt.Sprintf("Approval required for %s", toolName)
	if len(options) > 0 {
		body += "\nReact with one of the listed options."
	}
	content := &event.MessageEventContent{
		MsgType:  event.MsgText,
		Body:     body,
		Mentions: &event.Mentions{},
	}
	if ctx.TargetEvent != "" {
		content.RelatesTo = &event.RelatesTo{Type: event.RelReference, EventID: id.EventID(ctx.TargetEvent)}
	}
	extra := map[string]any{
		"com.beeper.ai.approval": map[string]any{
			"id":         ctx.ID,
			"toolCallId": ctx.ToolCallID,
			"toolName":   toolName,
			"threadId":   ctx.ThreadID,
			"runId":      ctx.RunID,
			"messageId":  ctx.MessageID,
			"approval": agui.ToolApproval{
				ID:            ctx.ID,
				NeedsApproval: true,
			},
			"reactions": ReactionOptionsAsAny(options),
		},
	}
	return content, extra
}

func ReactionOptionsAsAny(options []aistream.ReactionOption[agui.ToolApprovalResponse]) []any {
	out := make([]any, 0, len(options))
	for _, option := range options {
		out = append(out, map[string]any{
			"id":     option.ID,
			"label":  option.Label,
			"values": option.Values,
			"value":  option.Value,
		})
	}
	return out
}
