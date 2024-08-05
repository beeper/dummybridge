package connector

import (
	"strconv"

	"maunium.net/go/mautrix/bridgev2/commands"
	"maunium.net/go/mautrix/event"
)

var AllCommands = []commands.CommandHandler{
	NewRoomCommand,
	GhostsCommand,
	MessagesCommand,
}

var DummyHelpsection = commands.HelpSection{
	Name:  "Dummy",
	Order: 99,
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
