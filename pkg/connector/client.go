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
	"go.mau.fi/util/exsync"
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

	approvalSelectionsOnce sync.Once
	approvalSelections     *exsync.Map[string, string]
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
	aiGhostName      string           = "AI"
	aiPortalIDPrefix string           = "ai-"
	dummyAIAgentName string           = "Dummy"
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

	if msg.Portal != nil && (isAIPortalID(msg.Portal.ID) || isAIDemoCommandContent(msg.Content)) {
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
	key := aistream.NormalizeReaction(msg.Content.RelatesTo.Key)
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
	if isApprovalOptionReaction(msg) {
		return &database.Reaction{}, nil
	}
	approvalID := string(msg.TargetMessage.ID)
	if !strings.HasPrefix(approvalID, "approval-") {
		return &database.Reaction{}, nil
	}
	reaction := aistream.NormalizeReaction(msg.Content.RelatesTo.Key)
	selected, ok := aistream.ResolveApprovalChoice(aistream.DefaultApprovalChoices(), reaction)
	if !ok {
		return &database.Reaction{}, nil
	}
	response := aistream.ApprovalResponseForChoice(approvalID, selected)

	selectedKey, firstResolution := dc.resolveApprovalOnce(approvalID, reaction)
	dc.cleanupApprovalReactions(ctx, msg.Portal, networkid.MessageID(approvalID), selectedKey, reaction, msg)
	if !firstResolution {
		log.Info().
			Str("approval_id", approvalID).
			Str("reaction", reaction).
			Str("selected_reaction", selectedKey).
			Msg("Ignoring duplicate dummy AI approval reaction")
		return &database.Reaction{}, nil
	}
	portal := msg.Portal
	target := msg.TargetMessage
	dc.wg.Add(1)
	go func() {
		defer dc.wg.Done()
		dc.queueAIApprovalResponse(dc.ctx, portal, target, response)
	}()

	logger := log.Info().
		Str("approval_id", approvalID).
		Str("reaction", reaction).
		Str("choice", selected.Key).
		Bool("approved", response.Approved)
	if msg.Event != nil {
		logger = logger.Stringer("sender", msg.Event.Sender)
	}
	logger.Msg("Resolved dummy AI approval from Matrix reaction")

	return &database.Reaction{}, nil
}

func isApprovalOptionReaction(msg *bridgev2.MatrixReaction) bool {
	if msg == nil || msg.Event == nil {
		return false
	}
	_, ok := msg.Event.Content.Raw["com.beeper.ai.approval_option"]
	return ok
}

func (dc *DummyClient) resolveApprovalOnce(approvalID, selectedKey string) (string, bool) {
	dc.approvalSelectionsOnce.Do(func() {
		dc.approvalSelections = exsync.NewMap[string, string]()
	})
	selected, alreadyResolved := dc.approvalSelections.GetOrSet(approvalID, selectedKey)
	return selected, !alreadyResolved
}

func (dc *DummyClient) cleanupApprovalReactions(ctx context.Context, portal *bridgev2.Portal, approvalMessageID networkid.MessageID, selectedKey, reactionKey string, msg *bridgev2.MatrixReaction) {
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
			Bridge:  reaction.SenderID == dummyAISenderForPortal(portal),
		})
	}
	if msg != nil && msg.Event != nil && msg.Event.ID != "" {
		senderID := string(msg.Event.Sender)
		if msg.PreHandleResp != nil && msg.PreHandleResp.SenderID != "" {
			senderID = string(msg.PreHandleResp.SenderID)
		}
		events = append(events, aistream.ReactionEvent{
			EventID: string(msg.Event.ID),
			Sender:  senderID,
			Key:     reactionKey,
		})
	}
	sender := dummyAISenderForPortal(portal)
	cleanup := aistream.CleanupApprovalReactions(aistream.DefaultApprovalChoices(), selectedKey, events, string(sender))
	intent, ok := portal.GetIntentFor(ctx, bridgev2.EventSender{Sender: sender}, dc.UserLogin, bridgev2.RemoteEventMessageRemove)
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
		if err := dc.UserLogin.Bridge.DB.Reaction.Delete(ctx, msg.TargetReaction); err != nil {
			log.Warn().Err(err).Stringer("reaction_mxid", msg.TargetReaction.MXID).Msg("Failed to delete reaction on remove")
		}
	}
	return nil
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

func isAIDemoCommandContent(content *event.MessageEventContent) bool {
	if content == nil {
		return false
	}
	tokens := strings.Fields(strings.TrimSpace(content.Body))
	if len(tokens) == 0 {
		return false
	}
	switch strings.ToLower(tokens[0]) {
	case "help", "/help", "!help", "stream", "stream-tools":
		return true
	case "dummybridge":
		return len(tokens) > 1 && strings.EqualFold(tokens[1], "help")
	default:
		return false
	}
}

// ensureAISenderInvited queues a ChatInfoChange that adds the AI sender ghost
// to the given portal. The bridge's default portal generator can create
// portals with members=0, in which case the per-portal AI sender chosen by
// dummyAISenderForPortal is not actually a room member — sending the anchor
// from a non-member ghost would fail. Re-asserting an existing membership is
// a no-op for bridgev2, so it is safe to call for every AI run.
func (dc *DummyClient) ensureAISenderInvited(portal *bridgev2.Portal, sender networkid.UserID) {
	if dc == nil || dc.UserLogin == nil || portal == nil || sender == "" {
		return
	}
	if isAIPortalID(portal.ID) {
		return
	}
	changes := &bridgev2.ChatMemberList{MemberMap: bridgev2.ChatMemberMap{}}
	changes.MemberMap.Set(bridgev2.ChatMember{
		EventSender: bridgev2.EventSender{Sender: sender},
		Membership:  event.MembershipJoin,
		MemberEventExtra: map[string]any{
			"displayname": dummyAIAgentNameForPortal(portal),
		},
	})
	now := time.Now()
	dc.UserLogin.QueueRemoteEvent(&simplevent.ChatInfoChange{
		EventMeta: simplevent.EventMeta{
			Type:        bridgev2.RemoteEventChatInfoChange,
			PortalKey:   portal.PortalKey,
			Sender:      bridgev2.EventSender{Sender: sender},
			Timestamp:   now,
			StreamOrder: now.UnixNano(),
		},
		ChatInfoChange: &bridgev2.ChatInfoChange{MemberChanges: changes},
	})
}

func dummyAISenderForPortal(portal *bridgev2.Portal) networkid.UserID {
	if portal == nil {
		return networkid.UserID(dummyAIAgentName)
	}
	if isAIPortalID(portal.ID) {
		return aiGhostID
	}
	return stablePortalUserIDByIndex(portal.ID, 0)
}

func dummyAIAgentNameForPortal(portal *bridgev2.Portal) string {
	if portal != nil && isAIPortalID(portal.ID) {
		return aiGhostName
	}
	return dummyAIAgentName
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
	sender := dummyAISenderForPortal(portal)
	agentName := dummyAIAgentNameForPortal(portal)
	var body string
	if inbound != nil {
		body = inbound.Body
	}
	plans, err := buildAIRunPlans(ctx, runID, string(portal.ID), body, now, string(sender), agentName)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to build AI runs")
		return
	}
	for _, plan := range plans {
		if plan.Run == nil {
			continue
		}
		placeholderID := networkid.MessageID(plan.Run.MessageID)
		effectiveCommand := plan.EffectiveCommand
		if effectiveCommand == "" {
			effectiveCommand = body
		}

		dc.wg.Add(1)
		go func(portal *bridgev2.Portal, sender networkid.UserID, messageID networkid.MessageID, run aistream.Run, command string, delay time.Duration) {
			defer dc.wg.Done()
			if delay > 0 {
				timer := time.NewTimer(delay)
				select {
				case <-timer.C:
				case <-dc.ctx.Done():
					timer.Stop()
					return
				}
			}
			dc.ensureAISenderInvited(portal, sender)
			anchorAt := time.Now()
			dc.UserLogin.QueueRemoteEvent(aibridgev2.Anchor(portal.PortalKey, sender, initialAIAnchorRun(run), anchorAt))
			dc.queueAIRunStreamAndMetadata(portal, sender, messageID, run, command, anchorAt)
		}(portal, sender, placeholderID, *plan.Run, effectiveCommand, plan.Delay)
	}
}

func initialAIAnchorRun(run aistream.Run) aistream.Run {
	run.Status = aistream.Status{State: "streaming"}
	run.Usage = agui.Usage{}
	run.Preview = aistream.Preview{}
	return run
}

func (dc *DummyClient) queueAIRunStreamAndMetadata(portal *bridgev2.Portal, sender networkid.UserID, messageID networkid.MessageID, run aistream.Run, command string, anchorAt time.Time) {
	targetEventID := dc.waitForMessageMXID(portal, messageID, 30*time.Second)
	if targetEventID == "" {
		log.Warn().
			Str("run_id", run.RunID).
			Str("message_id", string(messageID)).
			Msg("Timed out waiting for AI anchor Matrix event")
		return
	}
	dc.emitAIRunStream(portal, sender, messageID, targetEventID, run, command, 1, anchorAt)
}

// emitAIRunStream packs and emits one segment of an AI run — used both for
// the initial run and for any approval continuation. It queues approval
// prompts produced by the segment, repacks once approval event IDs are
// known, and finally emits the carriers and (if the run terminated) the
// final metadata edit.
func (dc *DummyClient) emitAIRunStream(portal *bridgev2.Portal, sender networkid.UserID, messageID networkid.MessageID, targetEventID id.EventID, run aistream.Run, command string, startSeq int, anchorAt time.Time) {
	carriers, err := aistream.PackRunFromSeq(run, string(targetEventID), aistream.CarrierBudgetBytes, startSeq)
	if err != nil {
		log.Warn().Err(err).Str("run_id", run.RunID).Msg("Failed to pack AI stream")
		return
	}
	carriers = splitCarriersForTimedEmission(carriers)
	nextSeq := aistream.NextSeq(carriers)
	approvalEventIDs := make(map[string]id.EventID, len(run.Prompts))
	for i, prompt := range run.Prompts {
		prompt.SeqStart = nextSeq + i*aistream.ApprovalSeqReservation
		ctx := dc.queueAIApprovalPrompt(portal, sender, run, prompt, targetEventID, command, time.Now())
		if approvalEventID := dc.waitForMessageMXID(portal, networkid.MessageID(ctx.ID), 10*time.Second); approvalEventID != "" {
			approvalEventIDs[ctx.ID] = approvalEventID
			log.Info().
				Str("run_id", run.RunID).
				Str("approval_id", ctx.ID).
				Stringer("approval_event_id", approvalEventID).
				Int("approval_seq_start", ctx.SeqStart).
				Msg("AI approval notice ready for reaction")
		} else {
			log.Warn().
				Str("run_id", run.RunID).
				Str("approval_id", ctx.ID).
				Int("approval_seq_start", ctx.SeqStart).
				Msg("Timed out waiting for AI approval notice Matrix event")
		}
	}
	if len(approvalEventIDs) > 0 {
		annotateApprovalEventIDs(&run, approvalEventIDs)
		carriers, err = aistream.PackRunFromSeq(run, string(targetEventID), aistream.CarrierBudgetBytes, startSeq)
		if err != nil {
			log.Warn().Err(err).Str("run_id", run.RunID).Msg("Failed to repack AI stream with approval event IDs")
			return
		}
		carriers = splitCarriersForTimedEmission(carriers)
	}
	dc.queuePackedAICarriers(portal, sender, targetEventID, run, carriers, startSeq, anchorAt)
	if len(run.Prompts) > 0 && run.Status.State == "streaming" {
		log.Info().
			Str("run_id", run.RunID).
			Str("message_id", string(messageID)).
			Int("approval_prompts", len(run.Prompts)).
			Msg("AI run paused for approval")
	}
	if run.Status.State != "streaming" {
		dc.queueAIRunFinalMetadata(portal, sender, messageID, run)
	}
}

func (dc *DummyClient) queuePackedAICarriers(portal *bridgev2.Portal, sender networkid.UserID, targetEventID id.EventID, run aistream.Run, carriers []aistream.Carrier, startSeq int, anchorAt time.Time) {
	streamStart := time.Now()
	// minCarrierTimestamp guarantees every carrier lands strictly after the
	// anchor message timestamp so Matrix room ordering keeps the anchor first
	// and downstream RelatesTo resolution can always find the parent event.
	minCarrierTimestamp := anchorAt.Add(time.Millisecond)
	if streamStart.Before(minCarrierTimestamp) {
		streamStart = minCarrierTimestamp
	}
	for i, carrier := range carriers {
		dc.sleepUntilCarrierTime(run, carrier, streamStart)
		now := time.Now()
		if now.Before(minCarrierTimestamp) {
			now = minCarrierTimestamp
		}
		minCarrierTimestamp = now.Add(time.Nanosecond)
		dc.UserLogin.QueueRemoteEvent(aibridgev2.Carrier(portal.PortalKey, sender, run, carrier, targetEventID, startSeq+i, now))
	}
}

func splitCarriersForTimedEmission(carriers []aistream.Carrier) []aistream.Carrier {
	out := make([]aistream.Carrier, 0, len(carriers))
	for _, carrier := range carriers {
		if len(carrier.Envelopes) <= 1 {
			out = append(out, carrier)
			continue
		}
		for _, env := range carrier.Envelopes {
			out = append(out, aistream.Carrier{Envelopes: []aistream.Envelope{env}})
		}
	}
	return out
}

func (dc *DummyClient) sleepUntilCarrierTime(run aistream.Run, carrier aistream.Carrier, streamStart time.Time) {
	target := carrierTimestamp(run, carrier, streamStart)
	if target.IsZero() {
		return
	}
	delay := time.Until(target)
	if delay <= 0 {
		return
	}
	timer := time.NewTimer(delay)
	select {
	case <-timer.C:
	case <-dc.ctx.Done():
		timer.Stop()
	}
}

func carrierTimestamp(run aistream.Run, carrier aistream.Carrier, streamStart time.Time) time.Time {
	base := runStartTimestamp(run)
	if base.IsZero() {
		return time.Time{}
	}
	var latest time.Time
	for _, env := range carrier.Envelopes {
		eventTime := eventTimestamp(env.Part)
		if eventTime.IsZero() {
			continue
		}
		if latest.IsZero() || eventTime.After(latest) {
			latest = eventTime
		}
	}
	if latest.IsZero() {
		return time.Time{}
	}
	return streamStart.Add(latest.Sub(base))
}

func runStartTimestamp(run aistream.Run) time.Time {
	for _, evt := range run.Events {
		if ts := eventTimestamp(evt); !ts.IsZero() {
			return ts
		}
	}
	return time.Time{}
}

func eventTimestamp(evt agui.Event) time.Time {
	raw, ok := evt["timestamp"]
	if !ok {
		return time.Time{}
	}
	var millis int64
	switch value := raw.(type) {
	case int64:
		millis = value
	case int:
		millis = int64(value)
	case int32:
		millis = int64(value)
	case float64:
		millis = int64(value)
	case json.Number:
		parsed, err := value.Int64()
		if err != nil {
			return time.Time{}
		}
		millis = parsed
	default:
		return time.Time{}
	}
	if millis <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(millis)
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
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for ctx.Err() == nil {
		select {
		case <-ctx.Done():
			return ""
		case <-ticker.C:
		}
		for _, receiver := range receivers {
			mxid := dc.lookupMessageMXID(ctx, receiver, messageID)
			if mxid != "" {
				return mxid
			}
		}
	}
	return ""
}

func (dc *DummyClient) lookupMessageMXID(ctx context.Context, receiver networkid.UserLoginID, messageID networkid.MessageID) id.EventID {
	message, err := dc.UserLogin.Bridge.DB.Message.GetFirstPartByID(ctx, receiver, messageID)
	if err != nil || message == nil {
		return ""
	}
	return message.MXID
}

func (dc *DummyClient) queueAIApprovalPrompt(portal *bridgev2.Portal, sender networkid.UserID, run aistream.Run, prompt aistream.ApprovalPrompt, targetEventID id.EventID, command string, timestamp time.Time) aistream.ApprovalContext {
	choices := aistream.DefaultApprovalChoices()
	approvalCtx := aistream.ApprovalContext{
		ID:               prompt.ID,
		ThreadID:         run.ThreadID,
		RunID:            run.RunID,
		MessageID:        run.MessageID,
		Command:          command,
		ToolCallID:       prompt.ToolCallID,
		ToolName:         prompt.ToolName,
		TargetEvent:      string(targetEventID),
		AgentID:          run.AgentID,
		AgentName:        run.AgentName,
		Model:            run.Model,
		SeqStart:         prompt.SeqStart,
		PriorApprovals:   approvalResponsesBeforePrompt(run.Events, prompt.ID),
		PreviewText:      run.Preview.Text,
		PreviewTruncated: run.Preview.Truncated,
	}
	dc.UserLogin.QueueRemoteEvent(aibridgev2.ApprovalPrompt(portal.PortalKey, sender, approvalCtx, timestamp))

	for i, choice := range choices {
		choice := choice
		dc.UserLogin.QueueRemoteEvent(aibridgev2.ApprovalOptionReaction(portal.PortalKey, sender, approvalCtx, choice, timestamp.Add(time.Duration(i+1)*time.Millisecond)))
	}
	return approvalCtx
}

func annotateApprovalEventIDs(run *aistream.Run, eventIDs map[string]id.EventID) {
	if run == nil || len(eventIDs) == 0 {
		return
	}
	for _, evt := range run.Events {
		if evt["type"] != agui.EventCustom || evt["name"] != agui.ApprovalCustomRequested {
			continue
		}
		value, _ := evt["value"].(map[string]any)
		if value == nil {
			continue
		}
		approvalID := aistream.ApprovalIDFromRequestedValue(value)
		eventID := eventIDs[approvalID]
		if eventID == "" {
			continue
		}
		aistream.SetApprovalRequestedEventID(value, string(eventID))
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
	run, err := buildAIApprovalContinuationRun(ctx, approvalCtx, response, now)
	if err != nil {
		log.Warn().Err(err).Str("approval_id", approvalCtx.ID).Msg("Failed to build AI approval continuation")
		return
	}
	targetEventID := id.EventID(approvalCtx.TargetEvent)
	if targetEventID == "" {
		log.Warn().Str("approval_id", approvalCtx.ID).Msg("Missing AI approval target event")
		return
	}
	sender := networkid.UserID(approvalCtx.AgentID)
	if sender == "" {
		sender = dummyAISenderForPortal(portal)
	}
	dc.ensureAISenderInvited(portal, sender)
	dc.emitAIRunStream(portal, sender, networkid.MessageID(approvalCtx.MessageID), targetEventID, run, approvalCtx.Command, approvalCtx.SeqStart, now)
	log.Info().
		Str("run_id", approvalCtx.RunID).
		Str("approval_id", approvalCtx.ID).
		Str("tool_call_id", approvalCtx.ToolCallID).
		Bool("approved", response.Approved).
		Bool("always", response.Always).
		Int("seq_start", approvalCtx.SeqStart).
		Str("state", run.Status.State).
		Int("pending_prompts", len(run.Prompts)).
		Msg("Queued AI approval continuation")
}

func buildAIApprovalContinuationRun(ctx context.Context, approvalCtx aistream.ApprovalContext, response agui.ToolApprovalResponse, now time.Time) (aistream.Run, error) {
	cmd, err := parseCommand(approvalCtx.Command)
	if err != nil {
		return aistream.Run{}, err
	}
	if response.ID == "" {
		response.ID = approvalCtx.ID
	}
	approvals := make(map[string]agui.ToolApprovalResponse, len(approvalCtx.PriorApprovals)+1)
	for _, prior := range approvalCtx.PriorApprovals {
		if prior.ID != "" {
			approvals[prior.ID] = prior
		}
	}
	approvals[approvalCtx.ID] = response
	run, err := buildAIRunFromCommandWithApprovals(ctx, approvalCtx.RunID, approvalCtx.ThreadID, now, cmd, approvalCtx.AgentID, approvalCtx.AgentName, approvals)
	if err != nil {
		return aistream.Run{}, err
	}
	if run == nil {
		return aistream.Run{}, fmt.Errorf("approval continuation produced no run")
	}
	start := approvalContinuationStart(run.Events, approvalCtx.ID)
	if start < 0 {
		return aistream.Run{}, fmt.Errorf("approval response event %q not found", approvalCtx.ID)
	}
	run.Events = append([]agui.Event(nil), run.Events[start:]...)
	run.RunID = approvalCtx.RunID
	run.ThreadID = approvalCtx.ThreadID
	run.MessageID = approvalCtx.MessageID
	run.ToolCallID = approvalCtx.ToolCallID
	run.ApprovalID = approvalCtx.ID
	// Keep only prompts that the continuation segment newly emitted (i.e.
	// approvals raised by tools that ran AFTER the resolved one). The
	// already-resolved approval has been removed from the event range above
	// and must not be queued again.
	run.Prompts = filterPendingPrompts(run.Prompts, approvalCtx.ID, run.Events)
	return *run, nil
}

func approvalResponsesBeforePrompt(events []agui.Event, promptID string) []agui.ToolApprovalResponse {
	if promptID == "" {
		return nil
	}
	var responses []agui.ToolApprovalResponse
	for _, evt := range events {
		if evt["type"] != agui.EventCustom {
			continue
		}
		name, _ := evt["name"].(string)
		value, _ := evt["value"].(map[string]any)
		if value == nil {
			continue
		}
		if name == agui.ApprovalCustomRequested && aistream.ApprovalIDFromRequestedValue(value) == promptID {
			return responses
		}
		if name != agui.ApprovalCustomResponded {
			continue
		}
		if response, ok := approvalResponseFromAny(value["approval"]); ok && response.ID != "" {
			responses = append(responses, response)
		}
	}
	return responses
}

func approvalResponseFromAny(value any) (agui.ToolApprovalResponse, bool) {
	switch typed := value.(type) {
	case agui.ToolApprovalResponse:
		return typed, typed.ID != ""
	case *agui.ToolApprovalResponse:
		if typed == nil {
			return agui.ToolApprovalResponse{}, false
		}
		return *typed, typed.ID != ""
	case map[string]any:
		return approvalResponseFromMap(typed)
	default:
		raw, err := json.Marshal(value)
		if err != nil {
			return agui.ToolApprovalResponse{}, false
		}
		var response agui.ToolApprovalResponse
		if err = json.Unmarshal(raw, &response); err != nil {
			return agui.ToolApprovalResponse{}, false
		}
		return response, response.ID != ""
	}
}

func approvalResponseFromMap(value map[string]any) (agui.ToolApprovalResponse, bool) {
	idValue, _ := value["id"].(string)
	if idValue == "" {
		return agui.ToolApprovalResponse{}, false
	}
	response := agui.ToolApprovalResponse{ID: idValue}
	if approved, ok := value["approved"].(bool); ok {
		response.Approved = approved
	}
	if always, ok := value["always"].(bool); ok {
		response.Always = always
	}
	if reason, ok := value["reason"].(string); ok {
		response.Reason = reason
	}
	return response, true
}

func filterPendingPrompts(prompts []aistream.ApprovalPrompt, resolvedID string, events []agui.Event) []aistream.ApprovalPrompt {
	if len(prompts) == 0 {
		return nil
	}
	requested := make(map[string]bool, len(events))
	for _, evt := range events {
		if evt["type"] != agui.EventCustom || evt["name"] != agui.ApprovalCustomRequested {
			continue
		}
		value, _ := evt["value"].(map[string]any)
		if id := aistream.ApprovalIDFromRequestedValue(value); id != "" {
			requested[id] = true
		}
	}
	out := prompts[:0]
	for _, prompt := range prompts {
		if prompt.ID == resolvedID {
			continue
		}
		if !requested[prompt.ID] {
			continue
		}
		out = append(out, prompt)
	}
	return out
}

func approvalContinuationStart(events []agui.Event, approvalID string) int {
	for i, evt := range events {
		if evt["type"] != agui.EventCustom || evt["name"] != agui.ApprovalCustomResponded {
			continue
		}
		value, _ := evt["value"].(map[string]any)
		approval, _ := value["approval"].(agui.ToolApprovalResponse)
		if approval.ID == approvalID {
			return i
		}
		if raw, ok := value["approval"].(map[string]any); ok {
			if idValue, _ := raw["id"].(string); idValue == approvalID {
				return i
			}
		}
	}
	return -1
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
	if ctx.ID == "" || ctx.ThreadID == "" || ctx.RunID == "" || ctx.MessageID == "" || ctx.Command == "" || ctx.ToolCallID == "" || ctx.TargetEvent == "" {
		return aistream.ApprovalContext{}, false
	}
	if ctx.SeqStart <= 0 {
		ctx.SeqStart = 1
	}
	return ctx, true
}

func (dc *DummyClient) queueAIRunFinalMetadata(portal *bridgev2.Portal, sender networkid.UserID, messageID networkid.MessageID, run aistream.Run) {
	dc.UserLogin.QueueRemoteEvent(aibridgev2.FinalMetadataEdit(portal.PortalKey, sender, messageID, run, time.Now()))
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
				MemberMap: bridgev2.ChatMemberMap{
					networkid.UserID(dc.UserLogin.ID): {
						EventSender: bridgev2.EventSender{
							IsFromMe: true,
							Sender:   networkid.UserID(dc.UserLogin.ID),
						},
						Membership: event.MembershipJoin,
						PowerLevel: ptr.Ptr(100),
					},
					aiGhostID: {
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
