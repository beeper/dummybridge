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

	"github.com/beeper/ai-bridge/pkg/ag-ui"
	"github.com/beeper/ai-bridge/pkg/ai-stream"
	aibridgev2 "github.com/beeper/ai-bridge/pkg/ai-stream/bridgev2"
	aimatrix "github.com/beeper/ai-bridge/pkg/ai-stream/matrix"
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
	aiRunSessionsMu        sync.Mutex
	aiRunSessions          map[string]*aiRunSession
}

type aiRunSession struct {
	Decisions map[string]aistream.ToolApprovalResponse
}

var _ bridgev2.NetworkAPI = (*DummyClient)(nil)
var _ bridgev2.IdentifierResolvingNetworkAPI = (*DummyClient)(nil)
var _ bridgev2.BackfillingNetworkAPI = (*DummyClient)(nil)
var _ bridgev2.DeleteChatHandlingNetworkAPI = (*DummyClient)(nil)
var _ bridgev2.MessageRequestAcceptingNetworkAPI = (*DummyClient)(nil)
var _ bridgev2.ReactionHandlingNetworkAPI = (*DummyClient)(nil)

const (
	dummyAIAgentName         string = "Dummy"
	defaultAIApprovalTimeout        = 5 * time.Minute
	demoStreamCarrierMaxSpan        = 750 * time.Millisecond
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

func (dc *DummyClient) clientContext() context.Context {
	if dc != nil && dc.ctx != nil {
		return dc.ctx
	}
	return context.Background()
}

func (dc *DummyClient) done() <-chan struct{} {
	return dc.clientContext().Done()
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
		dc.queueAIApprovalResponse(dc.clientContext(), portal, target, response)
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
	if dc == nil || dc.UserLogin == nil || dc.UserLogin.Bridge == nil || dc.UserLogin.Bridge.DB == nil || msg == nil || msg.TargetReaction == nil {
		return nil
	}
	if err := dc.UserLogin.Bridge.DB.Reaction.Delete(ctx, msg.TargetReaction); err != nil {
		return fmt.Errorf("failed to delete reaction on remove: %w", err)
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
	return stablePortalUserIDByIndex(portal.ID, 0)
}

func dummyAIAgentNameForPortal(portal *bridgev2.Portal) string {
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
		case <-dc.done():
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

type aiRunTarget struct {
	portal    *bridgev2.Portal
	bot       bridgev2.MatrixAPI
	roomID    id.RoomID
	threadID  string
	sender    networkid.UserID
	agentName string
}

func (dc *DummyClient) queueAIResponse(ctx context.Context, portal *bridgev2.Portal, inbound *event.MessageEventContent) {
	if portal == nil {
		return
	}
	dc.queueAIResponseToTarget(ctx, aiRunTarget{
		portal:    portal,
		threadID:  string(portal.ID),
		sender:    dummyAISenderForPortal(portal),
		agentName: dummyAIAgentNameForPortal(portal),
	}, inbound)
}

func (dc *DummyClient) queueAIResponseInRoom(ctx context.Context, bot bridgev2.MatrixAPI, roomID id.RoomID, inbound *event.MessageEventContent) {
	if bot == nil || roomID == "" {
		return
	}
	dc.queueAIResponseToTarget(ctx, aiRunTarget{
		bot:       bot,
		roomID:    roomID,
		threadID:  string(roomID),
		sender:    networkid.UserID(dummyAIAgentName),
		agentName: dummyAIAgentName,
	}, inbound)
}

func (dc *DummyClient) queueAIResponseToTarget(ctx context.Context, target aiRunTarget, inbound *event.MessageEventContent) {
	now := time.Now()
	runID := "run-" + string(randomMessageID())
	var body string
	if inbound != nil {
		body = inbound.Body
	}
	plans, err := buildAIRunPlans(ctx, runID, target.threadID, body, now, string(target.sender), target.agentName)
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
		go func(target aiRunTarget, messageID networkid.MessageID, run aistream.Run, command string, delay time.Duration) {
			defer dc.wg.Done()
			if delay > 0 {
				timer := time.NewTimer(delay)
				select {
				case <-timer.C:
				case <-dc.done():
					timer.Stop()
					return
				}
			}
			anchorAt := time.Now()
			targetEventID, err := target.sendAnchor(dc, initialAIAnchorRun(run), messageID, anchorAt)
			if err != nil {
				log.Warn().Err(err).Str("run_id", run.RunID).Msg("Failed to send AI anchor")
				return
			}
			dc.emitAIRunStream(target, messageID, targetEventID, run, command, 1, anchorAt)
		}(target, placeholderID, *plan.Run, effectiveCommand, plan.Delay)
	}
}

func initialAIAnchorRun(run aistream.Run) aistream.Run {
	run.Status = aistream.Status{State: "streaming"}
	run.Usage = agui.Usage{}
	run.Preview = aistream.Preview{}
	return run
}

// emitAIRunStream packs and emits one segment of an AI run — used both for
// the initial run and for any approval continuation. It queues approval
// prompts produced by the segment, repacks once approval event IDs are
// known, and finally emits the carriers and (if the run terminated) the
// final metadata edit.
func (dc *DummyClient) emitAIRunStream(target aiRunTarget, messageID networkid.MessageID, targetEventID id.EventID, run aistream.Run, command string, startSeq int, anchorAt time.Time) {
	sizingRun := run
	annotateApprovalEventIDs(&sizingRun, approvalEventIDPlaceholders(sizingRun.Prompts))
	carriers, err := aistream.PackRunByTimeFromSeq(sizingRun, startSeq, demoStreamCarrierMaxSpan)
	if err != nil {
		log.Warn().Err(err).Str("run_id", run.RunID).Msg("Failed to pack AI stream")
		return
	}
	nextSeq := aistream.NextSeq(carriers)
	approvalQueue := aistream.NewApprovalQueue(aistream.ApprovalTimeout{After: defaultAIApprovalTimeout})
	approvalQueue.AddAll(run.Prompts)
	activePrompt, hasActivePrompt := approvalQueue.Active()
	if pending := approvalQueue.Pending(); len(pending) > 0 {
		log.Warn().
			Str("run_id", run.RunID).
			Int("pending_approval_prompts", len(pending)).
			Msg("AI run produced multiple approval prompts; keeping one active interrupt and queueing the rest")
	}
	approvalEventIDs := make(map[string]id.EventID, 1)
	if hasActivePrompt {
		prompt := activePrompt
		prompt.SeqStart = nextSeq
		approvalCtx, approvalEventID, err := target.sendApprovalPrompt(dc, run, prompt, targetEventID, command, time.Now())
		if err == nil && approvalEventID != "" {
			target.scheduleApprovalTimeout(dc, approvalCtx, approvalQueue.Timeout())
			approvalEventIDs[approvalCtx.ID] = approvalEventID
			log.Info().
				Str("run_id", run.RunID).
				Str("approval_id", approvalCtx.ID).
				Stringer("approval_event_id", approvalEventID).
				Int("approval_seq_start", approvalCtx.SeqStart).
				Msg("AI approval notice ready for reaction")
		} else {
			log.Warn().
				Err(err).
				Str("run_id", run.RunID).
				Str("approval_id", approvalCtx.ID).
				Int("approval_seq_start", approvalCtx.SeqStart).
				Msg("Timed out waiting for AI approval notice Matrix event")
		}
	}
	if len(approvalEventIDs) > 0 {
		annotateApprovalEventIDs(&run, approvalEventIDs)
		carriers, err = aistream.PackRunByTimeFromSeq(run, startSeq, demoStreamCarrierMaxSpan)
		if err != nil {
			log.Warn().Err(err).Str("run_id", run.RunID).Msg("Failed to repack AI stream with approval event IDs")
			return
		}
		if actualNextSeq := aistream.NextSeq(carriers); actualNextSeq != nextSeq {
			log.Warn().
				Str("run_id", run.RunID).
				Int("expected_next_seq", nextSeq).
				Int("actual_next_seq", actualNextSeq).
				Msg("AI approval event ID repack changed stream sequence count")
			return
		}
	} else if hasActivePrompt {
		log.Info().
			Str("run_id", run.RunID).
			Msg("Sending approval stream without approval event IDs")
	}
	target.sendCarriers(dc, targetEventID, run, carriers, startSeq, anchorAt)
	if len(run.Prompts) > 0 && run.Status.State == "streaming" {
		log.Info().
			Str("run_id", run.RunID).
			Str("message_id", string(messageID)).
			Int("approval_prompts", len(run.Prompts)).
			Msg("AI run paused for approval")
	}
	if run.Status.State != "streaming" {
		target.sendFinal(dc, messageID, targetEventID, run, time.Now())
	}
}

func (dc *DummyClient) scheduleAIApprovalTimeout(portal *bridgev2.Portal, approvalMessageID networkid.MessageID, timeout aistream.ApprovalTimeout) {
	if dc == nil || portal == nil || approvalMessageID == "" || timeout.After <= 0 {
		return
	}
	dc.wg.Add(1)
	go func() {
		defer dc.wg.Done()
		timer := time.NewTimer(timeout.After)
		defer timer.Stop()
		select {
		case <-dc.clientContext().Done():
			return
		case <-timer.C:
		}
		approvalID := string(approvalMessageID)
		if _, firstResolution := dc.resolveApprovalOnce(approvalID, timeout.Reason); !firstResolution {
			return
		}
		ctx := dc.clientContext()
		approvalMessage, err := dc.lookupMessage(ctx, portal.Receiver, approvalMessageID)
		if err != nil || approvalMessage == nil {
			log.Warn().Err(err).Str("approval_id", approvalID).Msg("Timed-out AI approval message was not found")
			return
		}
		response := aistream.TimedOutApprovalResponse(approvalID)
		dc.queueAIApprovalResponse(ctx, portal, approvalMessage, response)
	}()
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

func (target aiRunTarget) sendAnchor(dc *DummyClient, run aistream.Run, messageID networkid.MessageID, timestamp time.Time) (id.EventID, error) {
	if target.portal != nil {
		dc.ensureAISenderInvited(target.portal, target.sender)
		dc.UserLogin.QueueRemoteEvent(aibridgev2.Anchor(target.portal.PortalKey, target.sender, run, timestamp))
		eventID := dc.waitForMessageMXID(target.portal, messageID, 30*time.Second)
		if eventID == "" {
			return "", fmt.Errorf("timed out waiting for AI anchor Matrix event")
		}
		return eventID, nil
	}
	content, extra := aimatrix.AnchorContent(run)
	return dc.sendAIMessageToRoom(target.bot, target.roomID, content, extra, timestamp)
}

func (target aiRunTarget) sendApprovalPrompt(dc *DummyClient, run aistream.Run, prompt aistream.ApprovalPrompt, targetEventID id.EventID, command string, timestamp time.Time) (aistream.ApprovalContext, id.EventID, error) {
	approvalCtx := approvalContextForPrompt(run, prompt, targetEventID, command)
	if target.portal != nil {
		approvalCtx = dc.queueAIApprovalPrompt(target.portal, target.sender, run, prompt, targetEventID, command, timestamp)
		eventID := dc.waitForMessageMXID(target.portal, networkid.MessageID(approvalCtx.ID), 10*time.Second)
		if eventID == "" {
			return approvalCtx, "", fmt.Errorf("timed out waiting for AI approval notice Matrix event")
		}
		return approvalCtx, eventID, nil
	}
	eventID, err := dc.sendAIApprovalPromptToRoom(target.bot, target.roomID, approvalCtx, timestamp)
	return approvalCtx, eventID, err
}

func (target aiRunTarget) scheduleApprovalTimeout(dc *DummyClient, approvalCtx aistream.ApprovalContext, timeout aistream.ApprovalTimeout) {
	if target.portal != nil {
		dc.scheduleAIApprovalTimeout(target.portal, networkid.MessageID(approvalCtx.ID), timeout)
		return
	}
	if dc == nil || target.bot == nil || target.roomID == "" || approvalCtx.ID == "" || timeout.After <= 0 {
		return
	}
	dc.wg.Add(1)
	go func() {
		defer dc.wg.Done()
		timer := time.NewTimer(timeout.After)
		defer timer.Stop()
		select {
		case <-dc.clientContext().Done():
			return
		case <-timer.C:
		}
		if _, firstResolution := dc.resolveApprovalOnce(approvalCtx.ID, timeout.Reason); !firstResolution {
			return
		}
		response := aistream.TimedOutApprovalResponse(approvalCtx.ID)
		approvals := dc.recordAIApprovalDecision(approvalCtx.RunID, response)
		run, err := buildAIApprovalContinuationRunWithApprovals(dc.clientContext(), approvalCtx, approvals, time.Now())
		if err != nil {
			log.Warn().Err(err).Str("approval_id", approvalCtx.ID).Msg("Failed to build timed-out AI approval continuation")
			return
		}
		dc.emitAIRunStream(target, networkid.MessageID(approvalCtx.MessageID), id.EventID(approvalCtx.TargetEvent), run, approvalCtx.Command, approvalCtx.SeqStart, time.Now())
	}()
}

func (target aiRunTarget) sendCarriers(dc *DummyClient, targetEventID id.EventID, run aistream.Run, carriers []aistream.Carrier, startSeq int, anchorAt time.Time) {
	if target.portal != nil {
		dc.queuePackedAICarriers(target.portal, target.sender, targetEventID, run, carriers, startSeq, anchorAt)
		return
	}
	dc.sendPackedAICarriersToRoom(target.bot, target.roomID, targetEventID, run, carriers, startSeq, anchorAt)
}

func (target aiRunTarget) sendFinal(dc *DummyClient, messageID networkid.MessageID, targetEventID id.EventID, run aistream.Run, timestamp time.Time) {
	if target.portal != nil {
		dc.queueAIRunFinalMetadata(target.portal, target.sender, messageID, run)
		return
	}
	dc.sendAIRunFinalToRoom(target.bot, target.roomID, targetEventID, run, timestamp)
}

func approvalContextForPrompt(run aistream.Run, prompt aistream.ApprovalPrompt, targetEventID id.EventID, command string) aistream.ApprovalContext {
	return aistream.ApprovalContext{
		ID:               prompt.ID,
		ThreadID:         run.ThreadID,
		RunID:            run.RunID,
		MessageID:        run.MessageID,
		Command:          command,
		ToolCallID:       prompt.ToolCallID,
		ToolName:         prompt.ToolName,
		Title:            prompt.Title,
		Description:      prompt.Description,
		PlanText:         prompt.PlanText,
		ExpiresAt:        prompt.ExpiresAt,
		Choices:          aistream.DefaultApprovalChoices(),
		TargetEvent:      string(targetEventID),
		AgentID:          run.AgentID,
		AgentName:        run.AgentName,
		Model:            run.Model,
		SeqStart:         prompt.SeqStart,
		PreviewText:      run.Preview.Text,
		PreviewTruncated: run.Preview.Truncated,
		Metadata:         prompt.Metadata,
	}
}

func (dc *DummyClient) sendAIApprovalPromptToRoom(bot bridgev2.MatrixAPI, roomID id.RoomID, approvalCtx aistream.ApprovalContext, timestamp time.Time) (id.EventID, error) {
	content, extra := aimatrix.ApprovalContent(approvalCtx, aistream.DefaultApprovalChoices())
	return dc.sendAIMessageToRoom(bot, roomID, content, extra, timestamp)
}

func (dc *DummyClient) sendPackedAICarriersToRoom(bot bridgev2.MatrixAPI, roomID id.RoomID, targetEventID id.EventID, run aistream.Run, carriers []aistream.Carrier, startSeq int, anchorAt time.Time) {
	streamStart := time.Now()
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
		content, extra := aimatrix.CarrierContent(run, carrier, targetEventID)
		if _, err := dc.sendAIMessageToRoom(bot, roomID, content, extra, now); err != nil {
			log.Warn().Err(err).Str("run_id", run.RunID).Int("carrier_index", startSeq+i).Msg("Failed to send AI stream carrier to Matrix room")
			return
		}
	}
}

func (dc *DummyClient) sendAIRunFinalToRoom(bot bridgev2.MatrixAPI, roomID id.RoomID, targetEventID id.EventID, run aistream.Run, timestamp time.Time) {
	content, extra := aimatrix.FinalContent(run)
	content.SetEdit(targetEventID)
	raw := map[string]any{
		"m.new_content":                 extra,
		"com.beeper.dont_render_edited": true,
	}
	if _, err := dc.sendAIMessageToRoom(bot, roomID, content, raw, timestamp); err != nil {
		log.Warn().Err(err).Str("run_id", run.RunID).Msg("Failed to send AI final edit to Matrix room")
	}
}

func (dc *DummyClient) sendAIMessageToRoom(bot bridgev2.MatrixAPI, roomID id.RoomID, content *event.MessageEventContent, extra map[string]any, timestamp time.Time) (id.EventID, error) {
	resp, err := bot.SendMessage(dc.clientContext(), roomID, event.EventMessage, &event.Content{
		Parsed: content,
		Raw:    extra,
	}, &bridgev2.MatrixSendExtra{Timestamp: timestamp})
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", nil
	}
	return resp.EventID, nil
}

func (dc *DummyClient) sleepUntilCarrierTime(run aistream.Run, carrier aistream.Carrier, streamStart time.Time) {
	target := aistream.CarrierTimestamp(run, carrier, streamStart)
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
	case <-dc.done():
		timer.Stop()
	}
}

func (dc *DummyClient) waitForMessageMXID(
	portal *bridgev2.Portal,
	messageID networkid.MessageID,
	timeout time.Duration,
) id.EventID {
	if dc == nil || dc.UserLogin == nil || dc.UserLogin.Bridge == nil || dc.UserLogin.Bridge.DB == nil || portal == nil {
		return ""
	}

	receivers := []networkid.UserLoginID{portal.Receiver}
	if dc.UserLogin.ID != "" && dc.UserLogin.ID != portal.Receiver {
		receivers = append(receivers, dc.UserLogin.ID)
	}
	perReceiverTimeout := timeout
	if perReceiverTimeout <= 0 {
		perReceiverTimeout = 5 * time.Second
	}
	if len(receivers) > 1 {
		perReceiverTimeout /= time.Duration(len(receivers))
		if perReceiverTimeout < time.Second {
			perReceiverTimeout = time.Second
		}
	}
	for _, receiver := range receivers {
		eventID, err := aibridgev2.WaitForMessageEventID(
			dc.clientContext(),
			dc.UserLogin.Bridge,
			receiver,
			messageID,
			networkid.PartID("0"),
			perReceiverTimeout,
		)
		if err == nil && eventID != "" {
			return eventID
		}
	}
	return ""
}

func (dc *DummyClient) lookupMessage(ctx context.Context, receiver networkid.UserLoginID, messageID networkid.MessageID) (*database.Message, error) {
	if dc == nil || dc.UserLogin == nil || dc.UserLogin.Bridge == nil || dc.UserLogin.Bridge.DB == nil {
		return nil, nil
	}
	return dc.UserLogin.Bridge.DB.Message.GetFirstPartByID(ctx, receiver, messageID)
}

func (dc *DummyClient) queueAIApprovalPrompt(portal *bridgev2.Portal, sender networkid.UserID, run aistream.Run, prompt aistream.ApprovalPrompt, targetEventID id.EventID, command string, timestamp time.Time) aistream.ApprovalContext {
	approvalCtx := approvalContextForPrompt(run, prompt, targetEventID, command)
	dc.UserLogin.QueueRemoteEvent(aibridgev2.ApprovalPrompt(portal.PortalKey, sender, approvalCtx, timestamp))
	return approvalCtx
}

func annotateApprovalEventIDs(run *aistream.Run, eventIDs map[string]id.EventID) {
	if run == nil || len(eventIDs) == 0 {
		return
	}
	for i := range run.Interrupts {
		eventID := eventIDs[run.Interrupts[i].ID]
		if eventID == "" {
			continue
		}
		aistream.SetApprovalInterruptEventID(&run.Interrupts[i], string(eventID))
	}
	for _, evt := range run.Events {
		if evt.Type() != agui.EventRunFinished {
			continue
		}
		annotateApprovalOutcomeEventIDs(evt, eventIDs)
	}
}

func annotateApprovalOutcomeEventIDs(evt agui.Event, eventIDs map[string]id.EventID) {
	switch outcome := evt.Get("outcome").(type) {
	case agui.RunFinishedOutcome:
		for i := range outcome.Interrupts {
			eventID := eventIDs[outcome.Interrupts[i].ID]
			if eventID == "" {
				continue
			}
			aistream.SetApprovalInterruptEventID(&outcome.Interrupts[i], string(eventID))
		}
		evt.Set("outcome", outcome)
	case *agui.RunFinishedOutcome:
		if outcome == nil {
			return
		}
		for i := range outcome.Interrupts {
			eventID := eventIDs[outcome.Interrupts[i].ID]
			if eventID == "" {
				continue
			}
			aistream.SetApprovalInterruptEventID(&outcome.Interrupts[i], string(eventID))
		}
	}
}

func approvalEventIDPlaceholders(prompts []aistream.ApprovalPrompt) map[string]id.EventID {
	if len(prompts) == 0 {
		return nil
	}
	placeholders := make(map[string]id.EventID, len(prompts))
	const placeholderEventID = "$approval_event_id_placeholder_padding_for_stable_ai_stream_sequence_000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000:beeper.local"
	for _, prompt := range prompts {
		if prompt.ID != "" {
			placeholders[prompt.ID] = id.EventID(placeholderEventID)
		}
	}
	return placeholders
}

func (dc *DummyClient) queueAIApprovalResponse(ctx context.Context, portal *bridgev2.Portal, approvalMessage *database.Message, response aistream.ToolApprovalResponse) {
	approvalCtx, ok := dc.approvalContextForMessage(ctx, portal, approvalMessage)
	if !ok {
		log.Warn().Str("approval_id", messageIDString(approvalMessage)).Msg("Missing AI approval metadata")
		return
	}
	if response.ID == "" {
		response.ID = approvalCtx.ID
	}
	now := time.Now()
	approvals := dc.recordAIApprovalDecision(approvalCtx.RunID, response)
	run, err := buildAIApprovalContinuationRunWithApprovals(ctx, approvalCtx, approvals, now)
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
	dc.emitAIRunStream(aiRunTarget{
		portal:    portal,
		threadID:  approvalCtx.ThreadID,
		sender:    sender,
		agentName: approvalCtx.AgentName,
	}, networkid.MessageID(approvalCtx.MessageID), targetEventID, run, approvalCtx.Command, approvalCtx.SeqStart, now)
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

func buildAIApprovalContinuationRunWithApprovals(ctx context.Context, approvalCtx aistream.ApprovalContext, approvals map[string]aistream.ToolApprovalResponse, now time.Time) (aistream.Run, error) {
	cmd, err := parseCommand(approvalCtx.Command)
	if err != nil {
		return aistream.Run{}, err
	}
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
	// Keep only prompts that the continuation segment newly emitted (i.e.
	// approvals raised by tools that ran AFTER the resolved one). The
	// already-resolved approval has been removed from the event range above
	// and must not be queued again.
	run.Prompts = filterPendingPrompts(run.Prompts, approvalCtx.ID, run.Events)
	run.Interrupts = filterPendingInterrupts(run.Interrupts, run.Prompts, run.Events)
	if len(run.Prompts) > 0 {
		run.ApprovalID = run.Prompts[0].ID
		run.ToolCallID = run.Prompts[0].ToolCallID
	} else {
		run.ApprovalID = ""
		run.ToolCallID = ""
	}
	return *run, nil
}

func filterPendingPrompts(prompts []aistream.ApprovalPrompt, resolvedID string, events []agui.Event) []aistream.ApprovalPrompt {
	if len(prompts) == 0 {
		return nil
	}
	requested := approvalInterruptIDsFromEvents(events)
	if len(requested) == 0 {
		return nil
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

func filterPendingInterrupts(interrupts []agui.Interrupt, prompts []aistream.ApprovalPrompt, events []agui.Event) []agui.Interrupt {
	if len(prompts) == 0 {
		return nil
	}
	pending := make(map[string]bool, len(prompts))
	for _, prompt := range prompts {
		pending[prompt.ID] = true
	}
	var out []agui.Interrupt
	for _, interrupt := range approvalInterruptsFromEvents(events) {
		if pending[interrupt.ID] {
			out = append(out, interrupt)
		}
	}
	if len(out) > 0 {
		return out
	}
	for _, interrupt := range interrupts {
		if pending[interrupt.ID] {
			out = append(out, interrupt)
		}
	}
	return out
}

func approvalInterruptIDsFromEvents(events []agui.Event) map[string]bool {
	requested := map[string]bool{}
	for _, interrupt := range approvalInterruptsFromEvents(events) {
		if interrupt.ID != "" {
			requested[interrupt.ID] = true
		}
	}
	return requested
}

func approvalInterruptsFromEvents(events []agui.Event) []agui.Interrupt {
	var interrupts []agui.Interrupt
	for _, evt := range events {
		if evt.Type() != agui.EventRunFinished {
			continue
		}
		switch outcome := evt.Get("outcome").(type) {
		case agui.RunFinishedOutcome:
			if outcome.Type != agui.OutcomeInterrupt {
				continue
			}
			interrupts = append(interrupts, outcome.Interrupts...)
		case *agui.RunFinishedOutcome:
			if outcome == nil || outcome.Type != agui.OutcomeInterrupt {
				continue
			}
			interrupts = append(interrupts, outcome.Interrupts...)
		}
	}
	return interrupts
}

func approvalContinuationStart(events []agui.Event, approvalID string) int {
	for i, evt := range events {
		if evt.Type() != agui.EventToolCallResult {
			continue
		}
		if toolResultApprovalID(evt) == approvalID {
			return i
		}
	}
	return -1
}

func toolResultApprovalID(evt agui.Event) string {
	content, _ := evt.Get("content").(string)
	if content == "" {
		return ""
	}
	result, ok := aistream.ParseApprovalToolResult(content)
	if !ok {
		return ""
	}
	return result.ApprovalID
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
		if nested, ok := typed[aistream.BeeperAIApprovalKey]; ok {
			return approvalContextFromAny(nested)
		}
		return validApprovalContext(approvalContextFromMap(typed))
	case json.RawMessage:
		return approvalContextFromJSON(typed)
	case []byte:
		return approvalContextFromJSON(typed)
	}
	return aistream.ApprovalContext{}, false
}

func approvalContextFromMap(raw map[string]any) aistream.ApprovalContext {
	return aistream.ApprovalContext{
		ID:               stringField(raw, "id"),
		ThreadID:         stringField(raw, "threadId"),
		RunID:            stringField(raw, "runId"),
		MessageID:        stringField(raw, "messageId"),
		Command:          stringField(raw, "command"),
		ToolCallID:       stringField(raw, "toolCallId"),
		ToolName:         stringField(raw, "toolName"),
		Title:            stringField(raw, "title"),
		Description:      stringField(raw, "description"),
		PlanText:         stringField(raw, "planText"),
		ExpiresAt:        stringField(raw, "expiresAt"),
		Choices:          approvalChoicesField(raw, "choices"),
		TargetEvent:      stringField(raw, "targetEvent"),
		AgentID:          stringField(raw, "agentId"),
		AgentName:        stringField(raw, "agentName"),
		Model:            stringField(raw, "model"),
		SeqStart:         intField(raw, "seqStart"),
		PreviewText:      stringField(raw, "previewText"),
		PreviewTruncated: boolField(raw, "previewTruncated"),
		Metadata:         mapField(raw, "metadata"),
	}
}

func approvalContextFromJSON(raw []byte) (aistream.ApprovalContext, bool) {
	var ctx aistream.ApprovalContext
	if err := json.Unmarshal(raw, &ctx); err == nil {
		if approvalCtx, ok := validApprovalContext(ctx); ok {
			return approvalCtx, true
		}
	}
	var wrapper map[string]any
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return aistream.ApprovalContext{}, false
	}
	return approvalContextFromAny(wrapper)
}

func stringField(raw map[string]any, key string) string {
	value, _ := raw[key].(string)
	return value
}

func intField(raw map[string]any, key string) int {
	switch value := raw[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}

func boolField(raw map[string]any, key string) bool {
	value, _ := raw[key].(bool)
	return value
}

func mapField(raw map[string]any, key string) map[string]any {
	switch value := raw[key].(type) {
	case map[string]any:
		return value
	default:
		return nil
	}
}

func approvalChoicesField(raw map[string]any, key string) []aistream.ApprovalChoice {
	switch value := raw[key].(type) {
	case []aistream.ApprovalChoice:
		return value
	case []any:
		choices := make([]aistream.ApprovalChoice, 0, len(value))
		for _, item := range value {
			rawChoice, ok := item.(map[string]any)
			if !ok {
				return nil
			}
			choices = append(choices, aistream.ApprovalChoice{
				Key:      stringField(rawChoice, "key"),
				Label:    stringField(rawChoice, "label"),
				Alias:    stringField(rawChoice, "alias"),
				Style:    stringField(rawChoice, "style"),
				Shortcut: stringField(rawChoice, "shortcut"),
			})
		}
		return choices
	default:
		return nil
	}
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

func (dc *DummyClient) ensureAIRunSession(runID string) {
	if dc == nil || runID == "" {
		return
	}
	dc.aiRunSessionsMu.Lock()
	defer dc.aiRunSessionsMu.Unlock()
	if dc.aiRunSessions == nil {
		dc.aiRunSessions = make(map[string]*aiRunSession)
	}
	if dc.aiRunSessions[runID] == nil {
		dc.aiRunSessions[runID] = &aiRunSession{Decisions: make(map[string]aistream.ToolApprovalResponse)}
	}
}

func (dc *DummyClient) recordAIApprovalDecision(runID string, response aistream.ToolApprovalResponse) map[string]aistream.ToolApprovalResponse {
	decisions := make(map[string]aistream.ToolApprovalResponse)
	if response.ID == "" {
		return decisions
	}
	if dc == nil || runID == "" {
		decisions[response.ID] = response
		return decisions
	}
	dc.aiRunSessionsMu.Lock()
	defer dc.aiRunSessionsMu.Unlock()
	if dc.aiRunSessions == nil {
		dc.aiRunSessions = make(map[string]*aiRunSession)
	}
	session := dc.aiRunSessions[runID]
	if session == nil {
		session = &aiRunSession{Decisions: make(map[string]aistream.ToolApprovalResponse)}
		dc.aiRunSessions[runID] = session
	}
	session.Decisions[response.ID] = response
	for id, decision := range session.Decisions {
		decisions[id] = decision
	}
	return decisions
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
		MemberMap: bridgev2.ChatMemberMap{
			networkid.UserID(dc.UserLogin.ID): {
				EventSender: bridgev2.EventSender{
					IsFromMe: true,
					Sender:   networkid.UserID(dc.UserLogin.ID),
				},
				Membership: event.MembershipJoin,
				PowerLevel: ptr.Ptr(50),
			},
			userID: {
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
