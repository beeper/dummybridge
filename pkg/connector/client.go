package connector

import (
	"context"
	"fmt"
	"sync"

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
		log.Info().Int("portals", dc.Connector.Config.Automation.Portals.Count).Msg("Generating portals after login")
		for range dc.Connector.Config.Automation.Portals.Count {
			if _, err := generatePortal(
				ctx,
				dc.Connector.br,
				dc.UserLogin,
				dc.Connector.Config.Automation.Portals.Members,
			); err != nil {
				panic(err)
			}
		}
		dc.wg.Done()
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
	return &event.RoomFeatures{}
}

func (dc *DummyClient) IsThisUser(ctx context.Context, userID networkid.UserID) bool {
	return networkid.UserID(dc.UserLogin.ID) == userID
}

func (dc *DummyClient) GetChatInfo(ctx context.Context, portal *bridgev2.Portal) (*bridgev2.ChatInfo, error) {
	return &bridgev2.ChatInfo{}, nil
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
	return &bridgev2.MatrixMessageResponse{
		DB: &database.Message{},
	}, nil
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
