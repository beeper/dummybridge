package connector

import (
	"context"
	"fmt"
	"strings"

	"go.mau.fi/util/ptr"
	"go.mau.fi/util/random"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/event"
)

func randomPortalID() networkid.PortalID {
	return networkid.PortalID(strings.ToLower(random.String(32)))
}

func randomUserID() networkid.UserID {
	return networkid.UserID(strings.ToLower(random.String(32)))
}

func randomMessageID() networkid.MessageID {
	return networkid.MessageID(strings.ToLower(random.String(32)))
}

func stablePortalUserIDByIndex(portalID networkid.PortalID, idx int) networkid.UserID {
	return networkid.UserID(fmt.Sprintf("%s-%d", portalID, idx))
}

func generatePortal(ctx context.Context, br *bridgev2.Bridge, login *bridgev2.UserLogin, members int) (*bridgev2.Portal, error) {
	portalID := randomPortalID()
	portalKey := networkid.PortalKey{
		ID:       portalID,
		Receiver: login.ID,
	}

	portal, err := br.GetPortalByKey(ctx, portalKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get portal by key: %w", err)
	}

	chatInfo := bridgev2.ChatInfo{
		CanBackfill: true,
		Members: &bridgev2.ChatMemberList{
			Members: []bridgev2.ChatMember{
				{
					EventSender: bridgev2.EventSender{
						IsFromMe: true,
						Sender:   networkid.UserID(login.ID),
					},
					Membership: event.MembershipJoin,
					PowerLevel: ptr.Ptr(100),
				},
			},
		},
	}

	for i := 0; i < members; i++ {
		userID := stablePortalUserIDByIndex(portalID, i)
		_, err := br.GetGhostByID(ctx, userID)
		if err != nil {
			return nil, fmt.Errorf("failed to get ghost by id: %w", err)
		}

		chatInfo.Members.Members = append(chatInfo.Members.Members, bridgev2.ChatMember{
			EventSender: bridgev2.EventSender{
				Sender: userID,
			},
			Membership: event.MembershipJoin,
		})
	}

	return portal, portal.CreateMatrixRoom(ctx, login, &chatInfo)
}
