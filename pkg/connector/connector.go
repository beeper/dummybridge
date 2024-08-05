package connector

import (
	"context"
	_ "embed"
	"time"

	"go.mau.fi/util/configupgrade"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/commands"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/id"
)

type DummyConnector struct {
	br      *bridgev2.Bridge
	Started time.Time

	Config Config
}

type Config struct {
	Automation struct {
		OpenManagementRoom bool `yaml:"open_management_room"`
		Login              bool `yaml:"login"`
		Portals            struct {
			Count    int `yaml:"count"`
			Members  int `yaml:"members"`
			Messages int `yaml:"messages"`
		} `yaml:"portals"`
		Backfill struct {
			Timelimit time.Duration `yaml:"timelimit"`
		} `yaml:"backfill"`
	} `yaml:"automation"`
}

var _ bridgev2.NetworkConnector = (*DummyConnector)(nil)

func (dc *DummyConnector) Init(bridge *bridgev2.Bridge) {
	dc.br = bridge
	bridge.Commands.(*commands.Processor).AddHandlers(AllCommands...)
}

func (dc *DummyConnector) startupAutomation(ctx context.Context) error {
	portals, err := dc.br.DB.Portal.GetAll(ctx)
	if err != nil {
		return err
	}

	for userID, perm := range dc.br.Config.Permissions {
		// only do things for admins
		if !perm.Admin {
			continue
		}

		log := dc.br.Log.With().Str("phase", "automation").Str("user_id", userID).Logger()
		log.Info().Msg("Doing startup automation for user")

		// FIXME: t check if this is a valid mxid and not a pattern
		user, err := dc.br.GetUserByMXID(ctx, id.UserID(userID))
		if err != nil {
			log.Warn().Err(err).Msg("Couldn't find user by mxid, skipping")
			continue
		}

		if dc.Config.Automation.OpenManagementRoom {
			if roomID, err := user.GetManagementRoom(ctx); err != nil {
				log.Warn().Err(err).Msg("Failed to open management room")
			} else {
				log.Info().Stringer("room_id", roomID).Msg("Opened management room")
			}
		}

		// if we have a defaut login, do not create a new one
		login := user.GetDefaultLogin()
		if login == nil {
			log.Info().Msg("Logging in to dummy network")
			login, err = user.NewLogin(ctx, &database.UserLogin{
				ID:         networkid.UserLoginID(userID),
				BridgeID:   networkid.BridgeID(userID),
				RemoteName: userID,
			}, nil)
			if err != nil {
				return err
			}
		}

		log.Info().Int("portals", dc.Config.Automation.Portals.Count).Msg("Ensuring portals")
		for i := len(portals); i < dc.Config.Automation.Portals.Count; i++ {
			_, err = generatePortal(ctx, dc.br, login, dc.Config.Automation.Portals.Members)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (dc *DummyConnector) Start(ctx context.Context) error {
	dc.Started = time.Now()

	go func() {
		err := dc.startupAutomation(context.Background())
		if err != nil {
			dc.br.Log.Err(err).Msg("Startup automation failed")
		}
	}()
	return nil
}

func (dc *DummyConnector) GetCapabilities() *bridgev2.NetworkGeneralCapabilities {
	return &bridgev2.NetworkGeneralCapabilities{}
}

func (dc *DummyConnector) GetName() bridgev2.BridgeName {
	return bridgev2.BridgeName{
		DisplayName:      "Dummy",
		NetworkURL:       "https://beeper.com",
		NetworkIcon:      "mxc://beeper.com/f6ec13a4953757f04c1714b43d1c8ec451e0bab1",
		NetworkID:        "dummy",
		BeeperBridgeType: "beeper.com/dummy",
	}
}

func (dc *DummyConnector) GetDBMetaTypes() database.MetaTypes {
	return database.MetaTypes{}
}

//go:embed example-config.yaml
var ExampleConfig string

func upgradeConfig(helper configupgrade.Helper) {
	helper.Copy(configupgrade.Bool, "automation", "open_management_room")
	helper.Copy(configupgrade.Bool, "automation", "login")
	helper.Copy(configupgrade.Int, "automation", "portals", "count")
	helper.Copy(configupgrade.Int, "automation", "portals", "members")
	helper.Copy(configupgrade.Int, "automation", "portals", "messages")
	helper.Copy(configupgrade.Str, "automation", "backfill", "timelimit")
}

func (dc *DummyConnector) GetConfig() (example string, data any, upgrader configupgrade.Upgrader) {
	return ExampleConfig, &dc.Config, configupgrade.SimpleUpgrader(upgradeConfig)
}

func (dc *DummyConnector) LoadUserLogin(ctx context.Context, login *bridgev2.UserLogin) error {
	login.Client = &DummyClient{
		UserLogin: login,
		Connector: dc,
	}
	return nil
}

func (dc *DummyConnector) GetLoginFlows() []bridgev2.LoginFlow {
	return []bridgev2.LoginFlow{{
		Name:        "Password",
		Description: "Log in with a password",
		ID:          "password",
	}, {
		Name:        "Cookies",
		Description: "Log in with extracted cookies",
		ID:          "cookies",
	}, {
		Name:        "Local storage",
		Description: "Log in with extracted local storage",
		ID:          "localstorage",
	}, {
		Name:        "Display and wait",
		Description: "Log in through a remote server",
		ID:          "displayandwait",
	}}
}

func (dc *DummyConnector) CreateLogin(ctx context.Context, user *bridgev2.User, flowID string) (bridgev2.LoginProcess, error) {
	return &DummyLogin{User: user, FlowID: flowID}, nil
}
