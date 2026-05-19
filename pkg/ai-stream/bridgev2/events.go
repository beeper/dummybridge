package aibridgev2

import (
	"context"
	"time"

	aistream "github.com/beeper/dummybridge/pkg/ai-stream"
	aimatrix "github.com/beeper/dummybridge/pkg/ai-stream/matrix"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/bridgev2/simplevent"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

func Anchor(portalKey networkid.PortalKey, sender networkid.UserID, run aistream.Run, timestamp time.Time) *simplevent.PreConvertedMessage {
	content, extra := aimatrix.AnchorContent(run)
	return &simplevent.PreConvertedMessage{
		EventMeta: simplevent.EventMeta{
			Type:        bridgev2.RemoteEventMessage,
			PortalKey:   portalKey,
			Sender:      bridgev2.EventSender{Sender: sender},
			Timestamp:   timestamp,
			StreamOrder: timestamp.UnixNano(),
		},
		Data: &bridgev2.ConvertedMessage{Parts: []*bridgev2.ConvertedMessagePart{{
			ID:      networkid.PartID("0"),
			Type:    event.EventMessage,
			Content: content,
			Extra:   extra,
		}}},
		ID: networkid.MessageID(run.MessageID),
	}
}

func Carrier(portalKey networkid.PortalKey, sender networkid.UserID, run aistream.Run, carrier aistream.Carrier, targetEventID id.EventID, index int, timestamp time.Time) *simplevent.PreConvertedMessage {
	content, extra := aimatrix.CarrierContent(carrier, targetEventID)
	return &simplevent.PreConvertedMessage{
		EventMeta: simplevent.EventMeta{
			Type:        bridgev2.RemoteEventMessage,
			PortalKey:   portalKey,
			Sender:      bridgev2.EventSender{Sender: sender},
			Timestamp:   timestamp,
			StreamOrder: timestamp.UnixNano(),
		},
		Data: &bridgev2.ConvertedMessage{Parts: []*bridgev2.ConvertedMessagePart{{
			ID:      networkid.PartID("0"),
			Type:    event.EventMessage,
			Content: content,
			Extra:   extra,
		}}},
		ID: networkid.MessageID(aistream.StreamTxnID(run.RunID, index)),
	}
}

func ApprovalPrompt(portalKey networkid.PortalKey, sender networkid.UserID, ctx aistream.ApprovalContext, timestamp time.Time) *simplevent.PreConvertedMessage {
	content, extra := aimatrix.ApprovalContent(ctx, aistream.DefaultApprovalOptions(ctx.ID))
	return &simplevent.PreConvertedMessage{
		EventMeta: simplevent.EventMeta{
			Type:        bridgev2.RemoteEventMessage,
			PortalKey:   portalKey,
			Sender:      bridgev2.EventSender{Sender: sender},
			Timestamp:   timestamp,
			StreamOrder: timestamp.UnixNano(),
		},
		Data: &bridgev2.ConvertedMessage{Parts: []*bridgev2.ConvertedMessagePart{{
			ID:      networkid.PartID("0"),
			Type:    event.EventMessage,
			Content: content,
			Extra:   extra,
			DBMetadata: map[string]any{
				"com.beeper.ai.approval": ctx,
			},
		}}},
		ID: networkid.MessageID(ctx.ID),
	}
}

func ApprovalOptionReaction[T any](portalKey networkid.PortalKey, sender networkid.UserID, ctx aistream.ApprovalContext, option aistream.ReactionOption[T], timestamp time.Time) *simplevent.Reaction {
	return &simplevent.Reaction{
		EventMeta: simplevent.EventMeta{
			Type:        bridgev2.RemoteEventReaction,
			PortalKey:   portalKey,
			Sender:      bridgev2.EventSender{Sender: sender},
			Timestamp:   timestamp,
			StreamOrder: timestamp.UnixNano(),
		},
		TargetMessage: networkid.MessageID(ctx.ID),
		EmojiID:       networkid.EmojiID(option.ID),
		Emoji:         option.Values[0],
		ExtraContent: map[string]any{
			"com.beeper.ai.approval_option": map[string]any{
				"approvalId": ctx.ID,
				"toolCallId": ctx.ToolCallID,
				"optionId":   option.ID,
				"value":      option.Value,
			},
		},
	}
}

func FinalMetadataEdit(portalKey networkid.PortalKey, sender networkid.UserID, messageID networkid.MessageID, run aistream.Run, timestamp time.Time) *simplevent.Message[*aistream.Run] {
	finalContent, finalExtra := aimatrix.AnchorContent(run)
	return &simplevent.Message[*aistream.Run]{
		EventMeta: simplevent.EventMeta{
			Type:        bridgev2.RemoteEventEdit,
			PortalKey:   portalKey,
			Sender:      bridgev2.EventSender{Sender: sender},
			Timestamp:   timestamp,
			StreamOrder: timestamp.UnixNano(),
		},
		Data:          &run,
		ID:            messageID,
		TargetMessage: messageID,
		ConvertEditFunc: func(ctx context.Context, portal *bridgev2.Portal, intent bridgev2.MatrixAPI, existing []*database.Message, data *aistream.Run) (*bridgev2.ConvertedEdit, error) {
			if len(existing) == 0 {
				return nil, nil
			}
			return &bridgev2.ConvertedEdit{
				ModifiedParts: []*bridgev2.ConvertedEditPart{{
					Part:    existing[0],
					Type:    event.EventMessage,
					Content: finalContent,
					Extra:   finalExtra,
					TopLevelExtra: map[string]any{
						"com.beeper.dont_render_edited": true,
					},
				}},
			}, nil
		},
	}
}
