package connector

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"go.mau.fi/util/jsontime"
	"go.mau.fi/util/ptr"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/bridgev2/status"
	"maunium.net/go/mautrix/event"
)

type DummyClient struct {
	wg sync.WaitGroup

	UserLogin *bridgev2.UserLogin
	Connector *DummyConnector
}

var _ bridgev2.NetworkAPI = (*DummyClient)(nil)
var _ bridgev2.IdentifierResolvingNetworkAPI = (*DummyClient)(nil)
var _ bridgev2.BackfillingNetworkAPI = (*DummyClient)(nil)
var _ bridgev2.DeleteChatHandlingNetworkAPI = (*DummyClient)(nil)
var _ bridgev2.MessageRequestAcceptingNetworkAPI = (*DummyClient)(nil)

var dummyRoomCaps = &event.RoomFeatures{
	ID: "com.beeper.dummy.capabilities",

	Formatting: map[event.FormattingFeature]event.CapabilitySupportLevel{
		event.FmtBold:          event.CapLevelFullySupported,
		event.FmtItalic:        event.CapLevelFullySupported,
		event.FmtStrikethrough: event.CapLevelFullySupported,
		event.FmtInlineCode:    event.CapLevelFullySupported,
		event.FmtCodeBlock:     event.CapLevelFullySupported,
	},

	File: map[event.CapabilityMsgType]*event.FileFeatures{
		event.MsgImage: {MimeTypes: map[string]event.CapabilitySupportLevel{"*/*": event.CapLevelFullySupported}},
		event.MsgAudio: {MimeTypes: map[string]event.CapabilitySupportLevel{"*/*": event.CapLevelFullySupported}},
		event.MsgVideo: {MimeTypes: map[string]event.CapabilitySupportLevel{"*/*": event.CapLevelFullySupported}},
		event.MsgFile:  {MimeTypes: map[string]event.CapabilitySupportLevel{"*/*": event.CapLevelFullySupported}, Caption: event.CapLevelFullySupported},
	},

	MaxTextLength:       65536,
	LocationMessage:     event.CapLevelFullySupported,
	Reply:               event.CapLevelFullySupported,
	Edit:                event.CapLevelFullySupported,
	Delete:              event.CapLevelFullySupported,
	Reaction:            event.CapLevelFullySupported,
	ReactionCount:       1,
	ReadReceipts:        true,
	TypingNotifications: true,

	DeleteChat: true,
}

func (dc *DummyClient) Connect(ctx context.Context) {
	state := status.BridgeState{
		UserID:     dc.UserLogin.UserMXID,
		RemoteName: dc.UserLogin.RemoteName,
		StateEvent: status.StateConnected,
		Timestamp:  jsontime.UnixNow(),
	}
	dc.UserLogin.BridgeState.Send(state)

	dc.wg.Add(1)
	go func() {
		defer dc.wg.Done()
		log.Info().Int("portals", dc.Connector.Config.Automation.Portals.Count).Msg("Generating portals after login")
		for range dc.Connector.Config.Automation.Portals.Count {
			if _, err := generatePortal(
				ctx,
				dc.Connector.br,
				dc.UserLogin,
				dc.Connector.Config.Automation.Portals.Members,
			); errors.Is(err, context.Canceled) {
				return
			} else if err != nil {
				panic(err)
			}
		}
	}()
}

func (dc *DummyClient) Disconnect() {
	dc.wg.Wait()
}

func (dc *DummyClient) IsLoggedIn() bool {
	return true
}

func (dc *DummyClient) LogoutRemote(ctx context.Context) {}

func (dc *DummyClient) GetCapabilities(ctx context.Context, portal *bridgev2.Portal) *event.RoomFeatures {
	caps := *dummyRoomCaps
	if portal != nil && portal.MessageRequest {
		caps.MessageRequest = &event.MessageRequestFeatures{
			AcceptWithButton: event.CapLevelFullySupported,
		}
	}
	return &caps
}

func (dc *DummyClient) IsThisUser(ctx context.Context, userID networkid.UserID) bool {
	return networkid.UserID(dc.UserLogin.ID) == userID
}

func (dc *DummyClient) GetChatInfo(ctx context.Context, portal *bridgev2.Portal) (*bridgev2.ChatInfo, error) {
	portalIDPrefix := string(portal.ID)
	if len(portalIDPrefix) > 6 {
		portalIDPrefix = portalIDPrefix[:6]
	}
	portalName := fmt.Sprintf("Dummy Portal %s", portalIDPrefix)
	portalTopic := "DummyBridge test portal"

	roomType := portal.RoomType
	if roomType == "" {
		roomType = database.RoomTypeDM
	}

	chatInfo := &bridgev2.ChatInfo{
		Type: ptr.Ptr(roomType),
	}

	if portal.Name == "" {
		chatInfo.Name = ptr.Ptr(portalName)
	}
	if portal.Topic == "" {
		chatInfo.Topic = ptr.Ptr(portalTopic)
	}
	return chatInfo, nil
}

func (tc *DummyClient) GetUserInfo(ctx context.Context, ghost *bridgev2.Ghost) (*bridgev2.UserInfo, error) {
	name := ghost.Name
	if name == "" {
		name = string(ghost.ID)
		ghost.UpdateName(ctx, name)
	}
	return &bridgev2.UserInfo{
		Name: &name,
	}, nil
}

func (dc *DummyClient) HandleMatrixMessage(ctx context.Context, msg *bridgev2.MatrixMessage) (message *bridgev2.MatrixMessageResponse, err error) {
	// Dummy message requests are accepted by sending a message.
	if msg.Portal != nil && msg.Portal.MessageRequest {
		msg.Portal.MessageRequest = false
		msg.Portal.UpdateBridgeInfo(ctx)
		_ = msg.Portal.Save(ctx)
	}

	messageID := randomMessageID()
	if msg.Event != nil && msg.Event.Unsigned.TransactionID != "" {
		messageID = networkid.MessageID(msg.Event.Unsigned.TransactionID)
	}

	timestamp := time.Now()
	if msg.Event != nil && msg.Event.Timestamp != 0 {
		timestamp = time.UnixMilli(msg.Event.Timestamp)
	}

	return &bridgev2.MatrixMessageResponse{
		DB: &database.Message{
			ID:        messageID,
			SenderID:  networkid.UserID(dc.UserLogin.ID),
			Timestamp: timestamp,
		},
		StreamOrder: time.Now().UnixNano(),
	}, nil
}

func (dc *DummyClient) HandleMatrixDeleteChat(ctx context.Context, msg *bridgev2.MatrixDeleteChat) error {
	// bridgev2 will delete the portal + Matrix room after this returns nil.
	// For dummybridge, there's no separate remote-side deletion to do.
	return nil
}

func (dc *DummyClient) HandleMatrixAcceptMessageRequest(ctx context.Context, msg *bridgev2.MatrixAcceptMessageRequest) error {
	// Explicitly clear the request flag so bridgev2 doesn't need to.
	// This makes the behavior deterministic and keeps the state update close
	// to the connector implementation.
	if msg.Portal != nil && msg.Portal.MessageRequest {
		msg.Portal.MessageRequest = false
		msg.Portal.UpdateBridgeInfo(ctx)
		return msg.Portal.Save(ctx)
	}
	return nil
}

func (dc *DummyClient) ResolveIdentifier(ctx context.Context, identifier string, createChat bool) (*bridgev2.ResolveIdentifierResponse, error) {
	userID := networkid.UserID(identifier)
	portalID := randomPortalID()
	portalKey := networkid.PortalKey{
		ID:       portalID,
		Receiver: dc.UserLogin.ID,
	}

	ghost, err := dc.UserLogin.Bridge.GetGhostByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get ghost: %w", err)
	}
	portal, err := dc.UserLogin.Bridge.GetPortalByKey(ctx, portalKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get portal: %w", err)
	}
	ghostInfo, _ := dc.GetUserInfo(ctx, ghost)
	portalInfo, _ := dc.GetChatInfo(ctx, portal)
	portalInfo.Members = &bridgev2.ChatMemberList{
		Members: []bridgev2.ChatMember{
			{
				EventSender: bridgev2.EventSender{
					IsFromMe: true,
					Sender:   networkid.UserID(dc.UserLogin.ID),
				},
				Membership: event.MembershipJoin,
				PowerLevel: ptr.Ptr(50),
			},
			{
				EventSender: bridgev2.EventSender{
					Sender: userID,
				},
				Membership: event.MembershipJoin,
				PowerLevel: ptr.Ptr(50),
			},
		},
	}
	return &bridgev2.ResolveIdentifierResponse{
		Ghost:    ghost,
		UserID:   userID,
		UserInfo: ghostInfo,
		Chat: &bridgev2.CreateChatResponse{
			Portal:     portal,
			PortalKey:  portalKey,
			PortalInfo: portalInfo,
		},
	}, nil

}
