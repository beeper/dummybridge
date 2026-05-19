package connector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/beeper/dummybridge/pkg/ag-ui"
	"github.com/beeper/dummybridge/pkg/ai-stream"
	aibridgev2 "github.com/beeper/dummybridge/pkg/ai-stream/bridgev2"
	"github.com/rs/zerolog/log"
	"go.mau.fi/util/jsontime"
	"go.mau.fi/util/ptr"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/bridgev2/simplevent"
	"maunium.net/go/mautrix/bridgev2/status"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

type DummyClient struct {
	wg   sync.WaitGroup
	ctx  context.Context
	stop context.CancelFunc

	UserLogin *bridgev2.UserLogin
	Connector *DummyConnector

	approvalMu         sync.Mutex
	approvalSelections map[string]string
}

var _ bridgev2.NetworkAPI = (*DummyClient)(nil)
var _ bridgev2.IdentifierResolvingNetworkAPI = (*DummyClient)(nil)
var _ bridgev2.ContactListingNetworkAPI = (*DummyClient)(nil)
var _ bridgev2.BackfillingNetworkAPI = (*DummyClient)(nil)
var _ bridgev2.DeleteChatHandlingNetworkAPI = (*DummyClient)(nil)
var _ bridgev2.MessageRequestAcceptingNetworkAPI = (*DummyClient)(nil)
var _ bridgev2.ReactionHandlingNetworkAPI = (*DummyClient)(nil)

const (
	aiGhostID        networkid.UserID = "ai"
	aiGhostName                       = "AI"
	aiPortalIDPrefix                  = "ai-"
)

var delayedRemoteEchoPattern = regexp.MustCompile(`(?i)^remote-echo\s+delay\s+([0-9]+(?:ms|s|m|h))$`)

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

	MessageRequest: &event.MessageRequestFeatures{
		AcceptWithButton: event.CapLevelFullySupported,
	},

	DeleteChat: true,
}

func (dc *DummyClient) Connect(ctx context.Context) {
	dc.ctx, dc.stop = context.WithCancel(ctx)

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
				dc.ctx,
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
	if dc.stop != nil {
		dc.stop()
	}
	dc.wg.Wait()
}

func (dc *DummyClient) IsLoggedIn() bool {
	return true
}

func (dc *DummyClient) LogoutRemote(ctx context.Context) {}

func (dc *DummyClient) GetCapabilities(ctx context.Context, portal *bridgev2.Portal) *event.RoomFeatures {
	return dummyRoomCaps
}

func (dc *DummyClient) IsThisUser(ctx context.Context, userID networkid.UserID) bool {
	return networkid.UserID(dc.UserLogin.ID) == userID
}

func (dc *DummyClient) GetChatInfo(ctx context.Context, portal *bridgev2.Portal) (*bridgev2.ChatInfo, error) {
	if isAIPortalID(portal.ID) {
		roomType := database.RoomTypeDM
		return &bridgev2.ChatInfo{
			Name: ptr.Ptr(aiGhostName),
			Type: ptr.Ptr(roomType),
		}, nil
	}

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
	if ghost.ID == aiGhostID {
		name := aiGhostName
		isBot := true
		ghost.UpdateName(ctx, name)
		return &bridgev2.UserInfo{
			Identifiers: []string{string(aiGhostID), "AI"},
			Name:        &name,
			IsBot:       &isBot,
		}, nil
	}

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

	timestamp := time.Now()
	if msg.Event != nil && msg.Event.Timestamp != 0 {
		timestamp = time.UnixMilli(msg.Event.Timestamp)
	}

	behavior := getRemoteEchoBehavior(msg.Content)
	if behavior.fail {
		return nil, errors.New("dummy remote echo failure")
	}
	if behavior.pending {
		transactionID := getTransactionID(msg)
		dbMessage := &database.Message{
			ID:        randomMessageID(),
			SenderID:  networkid.UserID(dc.UserLogin.ID),
			Timestamp: timestamp,
		}
		msg.AddPendingToSave(dbMessage, transactionID, nil)
		dc.queueRemoteEcho(msg, transactionID, timestamp, behavior.delay)
		return &bridgev2.MatrixMessageResponse{
			DB:      dbMessage,
			Pending: true,
		}, nil
	}

	messageID := randomMessageID()
	if msg.Event != nil && msg.Event.Unsigned.TransactionID != "" {
		messageID = networkid.MessageID(msg.Event.Unsigned.TransactionID)
	}

	resp := &bridgev2.MatrixMessageResponse{
		DB: &database.Message{
			ID:        messageID,
			SenderID:  networkid.UserID(dc.UserLogin.ID),
			Timestamp: timestamp,
		},
		StreamOrder: time.Now().UnixNano(),
	}

	if msg.Portal != nil && isAIPortalID(msg.Portal.ID) {
		dc.queueAIResponse(ctx, msg.Portal, msg.Content)
	}

	return resp, nil
}

func (dc *DummyClient) PreHandleMatrixReaction(_ context.Context, msg *bridgev2.MatrixReaction) (bridgev2.MatrixReactionPreResponse, error) {
	if msg == nil || msg.Content == nil {
		return bridgev2.MatrixReactionPreResponse{}, nil
	}
	senderID := networkid.UserID("")
	if dc != nil && dc.UserLogin != nil {
		senderID = networkid.UserID(dc.UserLogin.ID)
	}
	key := normalizeApprovalReaction(msg.Content.RelatesTo.Key)
	return bridgev2.MatrixReactionPreResponse{
		SenderID:     senderID,
		EmojiID:      networkid.EmojiID(key),
		Emoji:        key,
		MaxReactions: 1,
	}, nil
}

func (dc *DummyClient) HandleMatrixReaction(ctx context.Context, msg *bridgev2.MatrixReaction) (*database.Reaction, error) {
	if dc == nil || dc.UserLogin == nil || msg == nil || msg.TargetMessage == nil || msg.Content == nil || msg.Portal == nil {
		return &database.Reaction{}, nil
	}
	approvalID := string(msg.TargetMessage.ID)
	if !strings.HasPrefix(approvalID, "approval-") {
		return &database.Reaction{}, nil
	}
	reaction := normalizeApprovalReaction(msg.Content.RelatesTo.Key)
	selected, ok := aistream.ResolveReaction(aistream.DefaultApprovalOptions(approvalID), reaction)
	if !ok {
		return &database.Reaction{}, nil
	}

	selectedKey, firstResolution := dc.resolveApprovalOnce(approvalID, reaction)
	dc.cleanupApprovalReactions(ctx, msg.Portal, networkid.MessageID(approvalID), selectedKey, msg)
	if !firstResolution {
		log.Info().
			Str("approval_id", approvalID).
			Str("reaction", reaction).
			Str("selected_reaction", selectedKey).
			Msg("Ignoring duplicate dummy AI approval reaction")
		return &database.Reaction{}, nil
	}
	dc.queueAIApprovalResponse(ctx, msg.Portal, msg.TargetMessage, selected.Value)

	log.Info().
		Str("approval_id", approvalID).
		Str("reaction", reaction).
		Bool("approved", selected.Value.Approved).
		Stringer("sender", msg.Event.Sender).
		Msg("Resolved dummy AI approval from Matrix reaction")

	return &database.Reaction{}, nil
}

func (dc *DummyClient) resolveApprovalOnce(approvalID, selectedKey string) (string, bool) {
	dc.approvalMu.Lock()
	defer dc.approvalMu.Unlock()
	if dc.approvalSelections == nil {
		dc.approvalSelections = make(map[string]string)
	}
	if existing := dc.approvalSelections[approvalID]; existing != "" {
		return existing, false
	}
	dc.approvalSelections[approvalID] = selectedKey
	return selectedKey, true
}

func (dc *DummyClient) cleanupApprovalReactions(ctx context.Context, portal *bridgev2.Portal, approvalMessageID networkid.MessageID, selectedKey string, msg *bridgev2.MatrixReaction) {
	if dc == nil || dc.UserLogin == nil || dc.UserLogin.Bridge == nil || dc.UserLogin.Bridge.DB == nil || portal == nil {
		return
	}
	reactions, err := dc.UserLogin.Bridge.DB.Reaction.GetAllToMessage(ctx, portal.Receiver, approvalMessageID)
	if err != nil {
		log.Warn().Err(err).Str("approval_id", string(approvalMessageID)).Msg("Failed to load approval reactions")
		return
	}
	events := make([]aistream.ReactionEvent, 0, len(reactions)+1)
	reactionByMXID := make(map[string]*database.Reaction, len(reactions))
	for _, reaction := range reactions {
		if reaction == nil || reaction.MXID == "" {
			continue
		}
		eventID := string(reaction.MXID)
		reactionByMXID[eventID] = reaction
		events = append(events, aistream.ReactionEvent{
			EventID: eventID,
			Sender:  string(reaction.SenderID),
			Key:     reaction.Emoji,
			Bridge:  reaction.SenderID == aiGhostID,
		})
	}
	if msg != nil && msg.Event != nil && msg.Event.ID != "" {
		events = append(events, aistream.ReactionEvent{
			EventID: string(msg.Event.ID),
			Sender:  string(msg.Event.Sender),
			Key:     selectedKey,
		})
	}
	cleanup := aistream.CleanupReactions(aistream.DefaultApprovalOptions(string(approvalMessageID)), selectedKey, events, string(aiGhostID))
	intent, ok := portal.GetIntentFor(ctx, bridgev2.EventSender{Sender: aiGhostID}, dc.UserLogin, bridgev2.RemoteEventMessageRemove)
	if !ok || intent == nil {
		log.Warn().Str("approval_id", string(approvalMessageID)).Msg("Failed to resolve AI sender intent for approval reaction cleanup")
		return
	}
	for _, reactionEventID := range cleanup.RedactReactionEvents {
		reactionMXID := id.EventID(reactionEventID)
		_, err := intent.SendMessage(ctx, portal.MXID, event.EventRedaction, &event.Content{
			Parsed: &event.RedactionEventContent{Redacts: reactionMXID},
		}, nil)
		if err != nil {
			log.Warn().Err(err).Stringer("reaction_mxid", reactionMXID).Msg("Failed to redact approval reaction")
			continue
		}
		if reaction := reactionByMXID[reactionEventID]; reaction != nil {
			if err := dc.UserLogin.Bridge.DB.Reaction.Delete(ctx, reaction); err != nil {
				log.Warn().Err(err).Stringer("reaction_mxid", reaction.MXID).Msg("Failed to delete approval reaction")
			}
		}
	}
}

func (dc *DummyClient) HandleMatrixReactionRemove(ctx context.Context, msg *bridgev2.MatrixReactionRemove) error {
	if dc != nil && dc.UserLogin != nil && dc.UserLogin.Bridge != nil && msg != nil && msg.TargetReaction != nil {
		_ = dc.UserLogin.Bridge.DB.Reaction.Delete(ctx, msg.TargetReaction)
	}
	return nil
}

func normalizeApprovalReaction(reaction string) string {
	return strings.TrimSpace(strings.ReplaceAll(reaction, "\ufe0f", ""))
}

func getTransactionID(msg *bridgev2.MatrixMessage) networkid.TransactionID {
	if msg.Event != nil && msg.Event.Unsigned.TransactionID != "" {
		return networkid.TransactionID(msg.Event.Unsigned.TransactionID)
	}
	return networkid.TransactionID(randomMessageID())
}

type remoteEchoBehavior struct {
	pending bool
	delay   time.Duration
	fail    bool
}

func getRemoteEchoBehavior(content *event.MessageEventContent) remoteEchoBehavior {
	if content == nil {
		return remoteEchoBehavior{}
	}
	body := strings.TrimSpace(content.Body)
	if strings.EqualFold(body, "remote-echo none") {
		return remoteEchoBehavior{pending: true}
	} else if strings.EqualFold(body, "remote-echo fail") {
		return remoteEchoBehavior{fail: true}
	}
	matches := delayedRemoteEchoPattern.FindStringSubmatch(body)
	if len(matches) != 2 {
		return remoteEchoBehavior{}
	}
	delay, err := time.ParseDuration(matches[1])
	if err != nil {
		return remoteEchoBehavior{}
	}
	return remoteEchoBehavior{pending: true, delay: delay}
}

func (dc *DummyClient) queueRemoteEcho(msg *bridgev2.MatrixMessage, transactionID networkid.TransactionID, timestamp time.Time, delay time.Duration) {
	if delay <= 0 || msg.Portal == nil {
		return
	}

	dc.wg.Add(1)
	go func() {
		defer dc.wg.Done()

		timer := time.NewTimer(delay)
		defer timer.Stop()

		select {
		case <-dc.ctx.Done():
			return
		case <-timer.C:
		}

		dc.UserLogin.QueueRemoteEvent(&simplevent.PreConvertedMessage{
			EventMeta: simplevent.EventMeta{
				Type:      bridgev2.RemoteEventMessage,
				PortalKey: msg.Portal.PortalKey,
				Sender: bridgev2.EventSender{
					IsFromMe:    true,
					SenderLogin: dc.UserLogin.ID,
					Sender:      networkid.UserID(dc.UserLogin.ID),
				},
				Timestamp:   timestamp,
				StreamOrder: time.Now().UnixNano(),
			},
			Data: &bridgev2.ConvertedMessage{Parts: []*bridgev2.ConvertedMessagePart{{
				Type:    event.EventMessage,
				Content: cloneMessageContent(msg.Content),
			}}},
			ID:            randomMessageID(),
			TransactionID: transactionID,
		})
	}()
}

func cloneMessageContent(content *event.MessageEventContent) *event.MessageEventContent {
	if content == nil {
		return nil
	}
	cloned := *content
	return &cloned
}

func (dc *DummyClient) queueAIResponse(ctx context.Context, portal *bridgev2.Portal, inbound *event.MessageEventContent) {
	if portal == nil {
		return
	}

	now := time.Now()
	runID := "run-" + string(randomMessageID())
	plans, err := buildAIRunPlans(ctx, runID, string(portal.ID), inboundBody(inbound), now)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to build AI runs")
		return
	}
	for _, plan := range plans {
		if plan.Run == nil {
			continue
		}
		timestamp := now.Add(plan.Delay)
		placeholderID := networkid.MessageID(plan.Run.MessageID)
		dc.UserLogin.QueueRemoteEvent(aibridgev2.Anchor(portal.PortalKey, aiGhostID, initialAIAnchorRun(*plan.Run), timestamp))

		go dc.queueAIRunStreamAndMetadata(portal, placeholderID, *plan.Run)
	}
}

func initialAIAnchorRun(run aistream.Run) aistream.Run {
	run.Status = aistream.Status{State: "streaming"}
	run.Usage = agui.Usage{}
	return run
}

func (dc *DummyClient) queueAIRunStreamAndMetadata(portal *bridgev2.Portal, messageID networkid.MessageID, run aistream.Run) {
	targetEventID := dc.waitForMessageMXID(portal, messageID, 30*time.Second)
	if targetEventID == "" {
		log.Warn().
			Str("run_id", run.RunID).
			Str("message_id", string(messageID)).
			Msg("Timed out waiting for AI anchor Matrix event")
		return
	}
	carriers, err := dc.queueAICarriers(portal, targetEventID, run, 1)
	if err != nil {
		log.Warn().Err(err).Str("run_id", run.RunID).Msg("Failed to pack AI stream")
		return
	}
	nextSeq := aistream.NextSeq(carriers)
	for i, prompt := range run.Prompts {
		prompt.SeqStart = nextSeq + i*10
		dc.queueAIApprovalPrompt(portal, run, prompt, targetEventID, time.Now())
	}
	dc.queueAIRunFinalMetadata(portal, messageID, run)
}

func (dc *DummyClient) queueAICarriers(portal *bridgev2.Portal, targetEventID id.EventID, run aistream.Run, startSeq int) ([]aistream.Carrier, error) {
	carriers, err := aistream.PackRunFromSeq(run, string(targetEventID), aistream.CarrierBudgetBytes, startSeq)
	if err != nil {
		return nil, err
	}
	for i, carrier := range carriers {
		now := time.Now()
		dc.UserLogin.QueueRemoteEvent(aibridgev2.Carrier(portal.PortalKey, aiGhostID, run, carrier, targetEventID, startSeq+i, now))
	}
	return carriers, nil
}

func (dc *DummyClient) waitForMessageMXID(
	portal *bridgev2.Portal,
	messageID networkid.MessageID,
	timeout time.Duration,
) id.EventID {
	if dc == nil || dc.UserLogin == nil || dc.UserLogin.Bridge == nil || dc.UserLogin.Bridge.DB == nil || portal == nil {
		return ""
	}
	parent := dc.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	receivers := []networkid.UserLoginID{portal.Receiver}
	if dc.UserLogin.ID != "" && dc.UserLogin.ID != portal.Receiver {
		receivers = append(receivers, dc.UserLogin.ID)
	}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for ctx.Err() == nil {
		for _, receiver := range receivers {
			mxid := dc.lookupMessageMXID(ctx, receiver, messageID)
			if mxid != "" {
				return mxid
			}
		}
		select {
		case <-ctx.Done():
			return ""
		case <-ticker.C:
		}
	}
	return ""
}

func (dc *DummyClient) lookupMessageMXID(ctx context.Context, receiver networkid.UserLoginID, messageID networkid.MessageID) id.EventID {
	var mxid id.EventID
	err := dc.UserLogin.Bridge.DB.Message.GetDB().QueryRow(
		ctx,
		`SELECT mxid FROM message WHERE bridge_id=$1 AND (room_receiver=$2 OR room_receiver='') AND id=$3 ORDER BY part_id ASC LIMIT 1`,
		dc.UserLogin.Bridge.DB.Message.BridgeID,
		receiver,
		messageID,
	).Scan(&mxid)
	if err != nil {
		return ""
	}
	return mxid
}

func (dc *DummyClient) queueAIApprovalPrompt(portal *bridgev2.Portal, run aistream.Run, prompt aistream.ApprovalPrompt, targetEventID id.EventID, timestamp time.Time) {
	reactions := aistream.DefaultApprovalOptions(prompt.ID)
	approvalCtx := aistream.ApprovalContext{
		ID:          prompt.ID,
		ThreadID:    run.ThreadID,
		RunID:       run.RunID,
		MessageID:   run.MessageID,
		ToolCallID:  prompt.ToolCallID,
		ToolName:    prompt.ToolName,
		TargetEvent: string(targetEventID),
		AgentID:     run.AgentID,
		AgentName:   run.AgentName,
		Model:       run.Model,
		SeqStart:    prompt.SeqStart,
	}
	dc.UserLogin.QueueRemoteEvent(aibridgev2.ApprovalPrompt(portal.PortalKey, aiGhostID, approvalCtx, timestamp))

	for i, reaction := range reactions {
		reaction := reaction
		dc.UserLogin.QueueRemoteEvent(aibridgev2.ApprovalOptionReaction(portal.PortalKey, aiGhostID, approvalCtx, reaction, timestamp.Add(time.Duration(i+1)*time.Millisecond)))
	}
}

func (dc *DummyClient) queueAIApprovalResponse(ctx context.Context, portal *bridgev2.Portal, approvalMessage *database.Message, response agui.ToolApprovalResponse) {
	approvalCtx, ok := dc.approvalContextForMessage(ctx, portal, approvalMessage)
	if !ok {
		log.Warn().Str("approval_id", messageIDString(approvalMessage)).Msg("Missing AI approval metadata")
		return
	}
	if response.ID == "" {
		response.ID = approvalCtx.ID
	}
	now := time.Now()
	run := aistream.ApprovalResponseRun(approvalCtx, response, now)
	targetEventID := id.EventID(approvalCtx.TargetEvent)
	if targetEventID == "" {
		log.Warn().Str("approval_id", approvalCtx.ID).Msg("Missing AI approval target event")
		return
	}
	if _, err := dc.queueAICarriers(portal, targetEventID, run, approvalCtx.SeqStart); err != nil {
		log.Warn().Err(err).Str("approval_id", approvalCtx.ID).Msg("Failed to queue AI approval response")
	}
}

func (dc *DummyClient) approvalContextForMessage(ctx context.Context, portal *bridgev2.Portal, message *database.Message) (aistream.ApprovalContext, bool) {
	var fetch func(context.Context, networkid.MessageID) (*database.Message, error)
	if dc != nil && dc.UserLogin != nil && dc.UserLogin.Bridge != nil && dc.UserLogin.Bridge.DB != nil && portal != nil {
		fetch = func(ctx context.Context, messageID networkid.MessageID) (*database.Message, error) {
			return dc.UserLogin.Bridge.DB.Message.GetFirstPartByID(ctx, portal.Receiver, messageID)
		}
	}
	return approvalContextForMessage(ctx, message, fetch)
}

func approvalContextForMessage(ctx context.Context, message *database.Message, fetch func(context.Context, networkid.MessageID) (*database.Message, error)) (aistream.ApprovalContext, bool) {
	if approvalCtx, ok := approvalContextFromMetadata(message); ok {
		return approvalCtx, true
	}
	if message == nil || message.ID == "" || fetch == nil {
		return aistream.ApprovalContext{}, false
	}
	fetched, err := fetch(ctx, message.ID)
	if err != nil {
		log.Warn().Err(err).Str("approval_id", string(message.ID)).Msg("Failed to reload AI approval message")
		return aistream.ApprovalContext{}, false
	}
	return approvalContextFromMetadata(fetched)
}

func approvalContextFromMetadata(message *database.Message) (aistream.ApprovalContext, bool) {
	if message == nil {
		return aistream.ApprovalContext{}, false
	}
	return approvalContextFromAny(message.Metadata)
}

func approvalContextFromAny(value any) (aistream.ApprovalContext, bool) {
	switch typed := value.(type) {
	case aistream.ApprovalContext:
		return validApprovalContext(typed)
	case *aistream.ApprovalContext:
		if typed == nil {
			return aistream.ApprovalContext{}, false
		}
		return validApprovalContext(*typed)
	case map[string]any:
		if nested, ok := typed["com.beeper.ai.approval"]; ok {
			return approvalContextFromAny(nested)
		}
	case *map[string]any:
		if typed == nil {
			return aistream.ApprovalContext{}, false
		}
		return approvalContextFromAny(*typed)
	case json.RawMessage:
		return approvalContextFromJSON(typed)
	case []byte:
		return approvalContextFromJSON(typed)
	case string:
		return approvalContextFromJSON([]byte(typed))
	}
	var ctx aistream.ApprovalContext
	raw, err := json.Marshal(value)
	if err != nil {
		return aistream.ApprovalContext{}, false
	}
	if err = json.Unmarshal(raw, &ctx); err != nil {
		return aistream.ApprovalContext{}, false
	}
	return validApprovalContext(ctx)
}

func approvalContextFromJSON(raw []byte) (aistream.ApprovalContext, bool) {
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err == nil {
		if approvalCtx, ok := approvalContextFromAny(decoded); ok {
			return approvalCtx, true
		}
	}
	var ctx aistream.ApprovalContext
	if err := json.Unmarshal(raw, &ctx); err != nil {
		return aistream.ApprovalContext{}, false
	}
	return validApprovalContext(ctx)
}

func messageIDString(message *database.Message) string {
	if message == nil {
		return ""
	}
	return string(message.ID)
}

func validApprovalContext(ctx aistream.ApprovalContext) (aistream.ApprovalContext, bool) {
	if ctx.ID == "" || ctx.ThreadID == "" || ctx.RunID == "" || ctx.MessageID == "" || ctx.ToolCallID == "" || ctx.TargetEvent == "" {
		return aistream.ApprovalContext{}, false
	}
	if ctx.SeqStart <= 0 {
		ctx.SeqStart = 1
	}
	return ctx, true
}

func (dc *DummyClient) queueAIRunFinalMetadata(portal *bridgev2.Portal, messageID networkid.MessageID, run aistream.Run) {
	dc.UserLogin.QueueRemoteEvent(aibridgev2.FinalMetadataEdit(portal.PortalKey, aiGhostID, messageID, run, time.Now()))
}

func inboundBody(content *event.MessageEventContent) string {
	if content == nil {
		return ""
	}
	return content.Body
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
	if isAIIdentifier(identifier) {
		return dc.resolveAIIdentifier(ctx, createChat)
	}

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

func (dc *DummyClient) GetContactList(ctx context.Context) ([]*bridgev2.ResolveIdentifierResponse, error) {
	contact, err := dc.resolveAIIdentifier(ctx, false)
	if err != nil {
		return nil, err
	}
	return []*bridgev2.ResolveIdentifierResponse{contact}, nil
}

func (dc *DummyClient) resolveAIIdentifier(ctx context.Context, createChat bool) (*bridgev2.ResolveIdentifierResponse, error) {
	ghost, err := dc.UserLogin.Bridge.GetGhostByID(ctx, aiGhostID)
	if err != nil {
		return nil, fmt.Errorf("failed to get AI ghost: %w", err)
	}
	userInfo, _ := dc.GetUserInfo(ctx, ghost)
	response := &bridgev2.ResolveIdentifierResponse{
		Ghost:    ghost,
		UserID:   aiGhostID,
		UserInfo: userInfo,
	}
	if !createChat {
		return response, nil
	}

	portalID := networkid.PortalID(aiPortalIDPrefix + string(randomPortalID()))
	portalKey := networkid.PortalKey{ID: portalID, Receiver: dc.UserLogin.ID}
	portal, err := dc.UserLogin.Bridge.GetPortalByKey(ctx, portalKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get AI portal: %w", err)
	}
	roomType := database.RoomTypeDM
	response.Chat = &bridgev2.CreateChatResponse{
		Portal:    portal,
		PortalKey: portalKey,
		PortalInfo: &bridgev2.ChatInfo{
			Name:        ptr.Ptr(aiGhostName),
			Topic:       ptr.Ptr("DummyBridge AI chat"),
			Type:        ptr.Ptr(roomType),
			CanBackfill: true,
			Members: &bridgev2.ChatMemberList{
				Members: []bridgev2.ChatMember{
					{
						EventSender: bridgev2.EventSender{
							IsFromMe: true,
							Sender:   networkid.UserID(dc.UserLogin.ID),
						},
						Membership: event.MembershipJoin,
						PowerLevel: ptr.Ptr(100),
					},
					{
						EventSender: bridgev2.EventSender{
							Sender: aiGhostID,
						},
						Membership: event.MembershipJoin,
						PowerLevel: ptr.Ptr(50),
						MemberEventExtra: map[string]any{
							"displayname":             aiGhostName,
							"com.beeper.ai.agent":     string(aiGhostID),
							"com.beeper.ai.model_id":  aistream.DefaultModel,
							"com.beeper.ai.protocol":  "ag-ui",
							"com.beeper.ai.static_ai": true,
						},
					},
				},
			},
		},
	}
	return response, nil
}

func isAIIdentifier(identifier string) bool {
	identifier = strings.TrimSpace(identifier)
	return strings.EqualFold(identifier, string(aiGhostID)) || strings.EqualFold(identifier, aiGhostName)
}

func isAIPortalID(portalID networkid.PortalID) bool {
	return strings.HasPrefix(string(portalID), aiPortalIDPrefix)
}
