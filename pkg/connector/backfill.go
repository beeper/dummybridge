package connector

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/event"
)

var _ bridgev2.BackfillingNetworkAPI = (*DummyClient)(nil)

func (dc *DummyClient) FetchMessages(ctx context.Context, fetchParams bridgev2.FetchMessagesParams) (resp *bridgev2.FetchMessagesResponse, err error) {
	resp = &bridgev2.FetchMessagesResponse{}

	if !dc.UserLogin.Bridge.Config.Backfill.Enabled {
		return
	} else if fetchParams.Portal == nil {
		return
	} else if time.Now().After(dc.Connector.Started.Add(dc.Connector.Config.Automation.Backfill.Timelimit)) {
		return
	}

	tsMassage := -time.Second
	if fetchParams.Forward {
		tsMassage *= -1
	}
	nextTs := time.Now()
	if fetchParams.AnchorMessage != nil {
		nextTs = fetchParams.AnchorMessage.Timestamp.Add(tsMassage)
	}

	for i := 0; i < fetchParams.Count; i++ {
		sender := stablePortalUserIDByIndex(fetchParams.Portal.ID, rand.Intn(dc.Connector.Config.Automation.Portals.Members))
		_, err := dc.UserLogin.Bridge.GetGhostByID(ctx, sender)
		if err != nil {
			return nil, fmt.Errorf("failed to get ghost by id: %w", err)
		}

		msg := bridgev2.BackfillMessage{
			ID: randomMessageID(),
			ConvertedMessage: &bridgev2.ConvertedMessage{
				Parts: []*bridgev2.ConvertedMessagePart{
					{
						Type: event.EventMessage,
						Content: &event.MessageEventContent{
							Body: string(sender),
						},
					},
				},
			},
			Timestamp: nextTs,
			Sender: bridgev2.EventSender{
				Sender: sender,
			},
		}

		resp.Messages = append(resp.Messages, &msg)
		nextTs = msg.Timestamp.Add(tsMassage)
	}

	// always claim we have more until timelimit is hit
	resp.HasMore = true
	return
}
