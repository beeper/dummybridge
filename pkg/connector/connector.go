package connector

import (
	"context"
	_ "embed"
	"time"

	"go.mau.fi/util/configupgrade"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/commands"
	"maunium.net/go/mautrix/bridgev2/database"
)

type DummyConnector struct {
	br      *bridgev2.Bridge
	Started time.Time

	Config Config
}

type Config struct {
	Automation struct {
		Portals struct {
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

func (dc *DummyConnector) Start(ctx context.Context) error {
	dc.Started = time.Now()
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
	return &DummyLogin{br: dc.br, Config: dc.Config, User: user, FlowID: flowID}, nil
}
