package connector

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"go.mau.fi/util/ptr"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/commands"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/bridgev2/simplevent"
	"maunium.net/go/mautrix/event"
)

var NewRequestDMCommand = &commands.FullHandler{
	Func: func(e *commands.Event) {
		login := e.User.GetDefaultLogin()
		sendInitialMessage := true
		for _, arg := range e.Args {
			if arg == "--no-message" {
				sendInitialMessage = false
			}
		}

		portal, err := createMessageRequestPortal(e.Ctx, e.Bridge, login, false, 1, sendInitialMessage)
		if err != nil {
			e.Reply(err.Error())
			return
		}
		e.Reply("Created DM message request %s", portal.MXID)
	},
	Name: "new-request-dm",
	Help: commands.HelpMeta{
		Description: "Create a DM message request room",
		Args:        "[--no-message]",
		Section:     DummyHelpsection,
	},
	RequiresLogin: true,
}

var NewRequestGroupCommand = &commands.FullHandler{
	Func: func(e *commands.Event) {
		login := e.User.GetDefaultLogin()

		remoteMembers := 2
		sendInitialMessage := true
		for _, arg := range e.Args {
			if arg == "--no-message" {
				sendInitialMessage = false
				continue
			}
			if n, err := strconv.Atoi(arg); err == nil {
				remoteMembers = n
			}
		}
		if remoteMembers < 2 {
			remoteMembers = 2
		}

		portal, err := createMessageRequestPortal(e.Ctx, e.Bridge, login, true, remoteMembers, sendInitialMessage)
		if err != nil {
			e.Reply(err.Error())
			return
		}
		e.Reply("Created group message request %s", portal.MXID)
	},
	Name: "new-request-group",
	Help: commands.HelpMeta{
		Description: "Create a group message request room",
		Args:        "[nmembers] [--no-message]",
		Section:     DummyHelpsection,
	},
	RequiresLogin: true,
}

func createMessageRequestPortal(
	ctx context.Context,
	br *bridgev2.Bridge,
	login *bridgev2.UserLogin,
	isGroup bool,
	remoteMembers int,
	sendInitialMessage bool,
) (*bridgev2.Portal, error) {
	portalID := randomPortalID()
	portalKey := networkid.PortalKey{ID: portalID, Receiver: login.ID}

	portal, err := br.GetPortalByKey(ctx, portalKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get portal by key: %w", err)
	}

	// Message request flag in bridgev2 is stored in the portal and surfaced via m.bridge.
	isMessageRequest := true
	roomType := database.RoomTypeDM
	var name *string
	if isGroup {
		roomType = database.RoomTypeGroupDM
		portalIDPrefix := string(portalID)
		if len(portalIDPrefix) > 6 {
			portalIDPrefix = portalIDPrefix[:6]
		}
		groupName := fmt.Sprintf("Dummy Request Group %s", portalIDPrefix)
		name = &groupName
	}
	portalTopic := "DummyBridge message request"

	chatInfo := bridgev2.ChatInfo{
		Name:           name,
		Topic:          &portalTopic,
		Type:           ptr.Ptr(roomType),
		MessageRequest: &isMessageRequest,
		CanBackfill:    true,
		Members: &bridgev2.ChatMemberList{Members: []bridgev2.ChatMember{{
			EventSender: bridgev2.EventSender{IsFromMe: true, Sender: networkid.UserID(login.ID)},
			Membership:  event.MembershipJoin,
			PowerLevel:  ptr.Ptr(100),
		}}},
	}

	firstGhost := stablePortalUserIDByIndex(portalID, 0)
	for i := 0; i < remoteMembers; i++ {
		userID := stablePortalUserIDByIndex(portalID, i)
		ghost, err := br.GetGhostByID(ctx, userID)
		if err != nil {
			return nil, fmt.Errorf("failed to get ghost by id: %w", err)
		}
		ghost.UpdateName(ctx, fmt.Sprintf("Dummy User %d", i+1))
		chatInfo.Members.Members = append(chatInfo.Members.Members, bridgev2.ChatMember{
			EventSender: bridgev2.EventSender{Sender: userID},
			Membership:  event.MembershipJoin,
		})
	}

	if err := portal.CreateMatrixRoom(ctx, login, &chatInfo); err != nil {
		return nil, err
	}

	if sendInitialMessage {
		// Send one incoming message so it shows up with a preview/unread state.
		login.QueueRemoteEvent(&simplevent.PreConvertedMessage{
			EventMeta: simplevent.EventMeta{
				Type:        bridgev2.RemoteEventMessage,
				PortalKey:   portal.PortalKey,
				Sender:      bridgev2.EventSender{Sender: firstGhost},
				Timestamp:   time.Now(),
				StreamOrder: time.Now().UnixNano(),
			},
			Data: &bridgev2.ConvertedMessage{Parts: []*bridgev2.ConvertedMessagePart{{
				Type: event.EventMessage,
				Content: &event.MessageEventContent{
					MsgType: event.MsgText,
					Body:    "Hey! This is a dummy message request.",
				},
			}}},
			ID: randomMessageID(),
		})
	}

	return portal, nil
}
