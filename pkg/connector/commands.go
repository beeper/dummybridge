package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/bridgev2/commands"
	bridgeMatrix "maunium.net/go/mautrix/bridgev2/matrix"
	"maunium.net/go/mautrix/bridgev2/status"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

var AllCommands = []commands.CommandHandler{
	SendStateCommand,
	NewRoomCommand,
	NewRequestDMCommand,
	NewRequestGroupCommand,
	GhostsCommand,
	MessagesCommand,
	FileCommand,
	KickMeCommand,
	CatCommand,
	CatAvatarCommand,
}

var DummyHelpsection = commands.HelpSection{
	Name:  "Dummy",
	Order: 99,
}

var SendStateCommand = &commands.FullHandler{
	Func: func(e *commands.Event) {
		if len(e.Args) == 0 {
			e.Reply("Missing state argument")
			return
		}

		stateEvent := status.BridgeStateEvent(e.Args[0])
		state := status.BridgeState{
			StateEvent: stateEvent,
			RemoteID:   "*",
		}

		for userID, perm := range e.Bridge.Config.Permissions {
			if !perm.Admin {
				continue
			}
			user, err := e.Bridge.GetUserByMXID(context.Background(), id.UserID(userID))
			if err != nil {
				e.Reply(fmt.Sprintf("Error getting user: %s", err.Error()))
				return
			}
			user.GetDefaultLogin().BridgeState.Send(state)
		}

		e.Reply("Generated states")
	},
	Name: "send-state",
	Help: commands.HelpMeta{
		Description: "Send bridge states",
		Args:        "[sevent]",
		Section:     DummyHelpsection,
	},
	RequiresLogin: true,
}

var NewRoomCommand = &commands.FullHandler{
	Func: func(e *commands.Event) {
		login := e.User.GetDefaultLogin()
		portal, err := generatePortal(e.Ctx, e.Bridge, login, 1)
		if err != nil {
			e.Reply(err.Error())
			return
		}

		e.Reply("Created portal %s with members", portal.MXID)
	},
	Name: "new-room",
	Help: commands.HelpMeta{
		Description: "Create a new room, optionally with members and messages",
		Args:        "[nmembers] [nmsgs]",
		Section:     DummyHelpsection,
	},
	RequiresLogin: true,
}

var GhostsCommand = &commands.FullHandler{
	Func: func(e *commands.Event) {
		if e.Portal == nil {
			e.Reply("Can only generate ghosts within a portal")
			return
		}

		var nGhosts uint64
		if len(e.Args) > 0 {
			nGhosts, _ = strconv.ParseUint(e.Args[0], 10, 16)
		}
		if nGhosts < 1 {
			nGhosts = 1
		}

		for i := 0; i < int(nGhosts); i++ {
			userID := randomUserID()
			ghost, err := e.Bridge.GetGhostByID(e.Ctx, userID)
			if err != nil {
				e.Reply(err.Error())
				return
			}
			err = ghost.Intent.EnsureJoined(e.Ctx, e.Portal.MXID)
			if err != nil {
				e.Reply(err.Error())
				return
			}
		}

		e.Reply("Generated %d ghosts", nGhosts)
	},
	Name: "ghosts",
	Help: commands.HelpMeta{
		Description: "Create ghosts to a room",
		Args:        "[nghosts]",
		Section:     DummyHelpsection,
	},
	RequiresLogin: true,
}

var MessagesCommand = &commands.FullHandler{
	Func: func(e *commands.Event) {
		if e.Portal == nil {
			e.Reply("Can only generate messages within a portal")
			return
		}

		var nMessages uint64
		if len(e.Args) > 0 {
			nMessages, _ = strconv.ParseUint(e.Args[0], 10, 16)
		}
		if nMessages < 1 {
			nMessages = 1
		}

		members, err := e.Bridge.Matrix.GetMembers(e.Ctx, e.Portal.MXID)
		if err != nil {
			e.Reply(err.Error())
			return
		}
		for member, evt := range members {
			if evt.Membership != event.MembershipJoin {
				continue
			}

			e.Reply(member.String())
		}

		e.Reply("Generated %d messages", nMessages)
	},
	Name: "messages",
	Help: commands.HelpMeta{
		Description: "Create messages in a room",
		Args:        "[nmsgs]",
		Section:     DummyHelpsection,
	},
	RequiresLogin: true,
}

var FileCommand = &commands.FullHandler{
	Func: func(e *commands.Event) {
		e.Reply("Generating file event in this room")

		var mediaData []byte
		mediaData = []byte("Test text file")
		mediaName := "test.txt"
		mediaMime := "text/plain"

		url, fi, err := e.Bot.UploadMedia(e.Ctx, e.RoomID, mediaData, mediaName, mediaMime)
		if err != nil {
			e.Reply(err.Error())
			return
		}

		content := event.Content{
			Parsed: &event.MessageEventContent{
				MsgType: event.MsgFile,
				URL:     url,
				Body:    "Caption for file " + mediaName,
				Info: &event.FileInfo{
					Size:     len(mediaData),
					MimeType: mediaMime,
				},
				File: fi,
			},
		}

		resp, err := e.Bot.SendMessage(e.Ctx, e.RoomID, event.EventMessage, &content, nil)
		if err != nil {
			e.Reply(err.Error())
			return
		}

		e.Reply(resp.EventID.String())
	},
	Name: "file",
	Help: commands.HelpMeta{
		Description: "Create boring file events in room",
		Section:     DummyHelpsection,
	},
}

var KickMeCommand = &commands.FullHandler{
	Func: func(e *commands.Event) {
		portal := e.Portal
		args := e.Args

		// Allow using this from the management room by specifying a room ID.
		if portal == nil {
			if len(args) == 0 {
				e.Reply("Usage: `$cmdprefix kick-me [reason...]` (in a portal room) or `$cmdprefix kick-me <room_id> [reason...]`")
				return
			}
			if !strings.HasPrefix(args[0], "!") {
				e.Reply("Usage: `$cmdprefix kick-me [reason...]` (in a portal room) or `$cmdprefix kick-me <room_id> [reason...]`")
				return
			}
			candidateRoomID := id.RoomID(args[0])
			var err error
			portal, err = e.Bridge.GetPortalByMXID(e.Ctx, candidateRoomID)
			if err != nil {
				e.Reply("Failed to get portal for room: %s", err)
				return
			} else if portal == nil {
				e.Reply("Room %s is not a portal room", candidateRoomID)
				return
			}
			args = args[1:]
		}

		kickerRemoteID := stablePortalUserIDByIndex(portal.ID, 0)
		ghost, err := e.Bridge.GetGhostByID(e.Ctx, kickerRemoteID)
		if err != nil {
			e.Reply("Failed to get ghost kicker: %s", err)
			return
		}

		if err := ghost.Intent.EnsureJoined(e.Ctx, portal.MXID); err != nil {
			e.Reply("Failed to join ghost kicker to room: %s", err)
			return
		}

		asIntent, ok := ghost.Intent.(*bridgeMatrix.ASIntent)
		if !ok {
			e.Reply("Unsupported ghost intent type: %T", ghost.Intent)
			return
		}

		// Best-effort: ensure the ghost has high enough PL to kick.
		// The kick itself is sent as a custom membership event so this generally isn't required,
		// but it makes behavior closer to a real kick flow.
		_, _ = asIntent.Matrix.SetPowerLevel(e.Ctx, portal.MXID, asIntent.GetMXID(), 100)

		reason := strings.TrimSpace(strings.Join(args, " "))
		req := &mautrix.ReqKickUser{UserID: e.User.MXID, Reason: reason}
		_, err = asIntent.Matrix.KickUser(e.Ctx, portal.MXID, req, map[string]interface{}{"com.beeper.dummybridge": true})
		if err != nil {
			e.Reply("Failed to kick you: %s", err)
			return
		}
	},
	Name: "kick-me",
	Help: commands.HelpMeta{
		Description: "Simulate being kicked from a portal room",
		Args:        "[room_id] [reason...]",
		Section:     DummyHelpsection,
	},
	RequiresLogin: true,
}

var catpions []string = []string{
	"You’ve cat to be kitten me!",
	"I’m feline fine!",
	"That was a total cat-astrophe.",
	"Cut the cat-itude!",
	"Let's go to Pawbucks and grab a few cat-puccinos.",
	"Meow you doing?",
	"My cat got fined for littering.",
	"You’re the cat’s paw-jamas!",
	"Meow you’re talking!",
	"I’m just kitten around.",
	"My cat sure is purr-suasive.",
	"How do you like me meow?",
	"Want a meowtini? Shaken, not purred, of course.",
	"Turn up the mewsic and let’s get this pawty started!",
	"Stop stressing meowt!",
	"These puns are just hiss-terical!",
	"My cat is often confused. You could say he gets pretty purr-plexed.",
	"Now wait just a meowment…",
	"My purr-fect cat thinks everyone is in-fur-rior to them!",
	"Let me put my thinking cat on.",
	"You're going to be my feline friend fur-ever!",
	"Don’t fur-get to buy more catnip!",
	"Looking good, feline good.",
	"My cat is going down in hiss-story as the best cat ever.",
	"Stop fighting! Just hiss and make up.",
	"Press paws and live in the meow.",
	"I love you, meow and forever.",
	"It was meant to be. You could say it was kitten in the stars.",
	"My cat has quite the purr-sonality!",
	"You're as purr-ty as a picture!",
	"With the right catitude, anything is pawsible!",
	"I can tell you have a secret. It’s kitten all over your face!",
	"I’m litter-ly in love with you.",
	"Happy purr-thday!",
	"I’m a total cat purr-son.",
	"Pass the paw-pcorn, please.",
}

var catClient http.Client = http.Client{
	Timeout: 10 * time.Second,
}

type catDesc struct {
	ID     string `json:"id"`
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

func searchCat(ctx context.Context) (catDesc, error) {
	// this is only for individual testing so we're not using the authenticated API
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.thecatapi.com/v1/images/search", nil)
	if err != nil {
		return catDesc{}, err
	}

	resp, err := catClient.Do(req)
	if err != nil {
		return catDesc{}, err
	}

	var catDescs []catDesc
	err = json.NewDecoder(resp.Body).Decode(&catDescs)
	if len(catDescs) > 0 {
		return catDescs[0], err
	}

	return catDesc{}, err
}

func getCat(ctx context.Context, url string) (string, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", nil, err
	}

	resp, err := catClient.Do(req)
	if err != nil {
		return "", nil, err
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, err
	}

	return resp.Header.Get("Content-Type"), data, nil
}

var CatCommand = &commands.FullHandler{
	Func: func(e *commands.Event) {
		e.Log.Debug().Msg("Searching for cat")

		catDesc, err := searchCat(e.Ctx)
		if err != nil {
			e.Reply(err.Error())
			return
		}

		mediaMime, mediaData, err := getCat(e.Ctx, catDesc.URL)
		if err != nil {
			e.Reply(err.Error())
			return
		}

		mediaName := catDesc.ID
		if strings.Contains(mediaMime, "png") {
			mediaName = mediaName + ".png"
		} else if strings.Contains(mediaMime, "jp") {
			mediaName = mediaName + ".jpg"
		} else if strings.Contains(mediaMime, "gif") {
			mediaName = mediaName + ".gif"
		} else if !strings.HasPrefix(mediaMime, "image/") {
			e.Reply("Failed to get a cat: %s", mediaMime)
			return
		}

		e.Log.Debug().Msg("Uploading cat")
		url, fi, err := e.Bot.UploadMedia(e.Ctx, e.RoomID, mediaData, mediaName, mediaMime)
		if err != nil {
			e.Reply(err.Error())
			return
		}

		catpion := catpions[rand.Intn(len(catpions)-1)]
		content := event.Content{
			Parsed: &event.MessageEventContent{
				MsgType: event.MsgImage,
				URL:     url,
				Body:    catpion,
				Info: &event.FileInfo{
					Size:     len(mediaData),
					Width:    catDesc.Width,
					Height:   catDesc.Height,
					MimeType: mediaMime,
				},
				File: fi,
			},
		}

		e.Log.Debug().Msg("Sending cat")

		e.Reply(catpion)
		_, err = e.Bot.SendMessage(e.Ctx, e.RoomID, event.EventMessage, &content, nil)
		if err != nil {
			e.Reply(err.Error())
			return
		}
	},
	Name: "cat",
	Help: commands.HelpMeta{
		Description: "You know if you need one",
		Section:     DummyHelpsection,
	},
}

var CatAvatarCommand = &commands.FullHandler{
	Func: func(e *commands.Event) {
		e.Log.Debug().Msg("Searching for cat")

		catDesc, err := searchCat(e.Ctx)
		if err != nil {
			e.Reply(err.Error())
			return
		}

		mediaMime, mediaData, err := getCat(e.Ctx, catDesc.URL)
		if err != nil {
			e.Reply(err.Error())
			return
		}

		mediaName := catDesc.ID
		if strings.Contains(mediaMime, "png") {
			mediaName = mediaName + ".png"
		} else if strings.Contains(mediaMime, "jp") {
			mediaName = mediaName + ".jpg"
		} else if strings.Contains(mediaMime, "gif") {
			mediaName = mediaName + ".gif"
		} else if !strings.HasPrefix(mediaMime, "image/") {
			e.Reply("Failed to get a cat: %s", mediaMime)
			return
		}

		e.Log.Debug().Msg("Uploading cat")
		url, _, err := e.Bot.UploadMedia(e.Ctx, e.RoomID, mediaData, mediaName, mediaMime)
		if err != nil {
			e.Reply(err.Error())
			return
		}

		e.Log.Debug().Msg("Sending cat state")
		content := event.Content{
			Parsed: &event.RoomAvatarEventContent{
				URL: url,
			},
		}
		_, err = e.Bot.SendState(e.Ctx, e.RoomID, event.StateRoomAvatar, "", &content, time.Now())
		if err != nil {
			e.Reply(err.Error())
			return
		}
	},
	Name: "cat-avatar",
	Help: commands.HelpMeta{
		Description: "Make this room look like a cat",
		Section:     DummyHelpsection,
	},
}
