package connector

import (
	"context"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"

	"go.mau.fi/util/jsonbytes"
	"go.mau.fi/util/jsontime"
	"go.mau.fi/util/random"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/bridgev2/status"
)

type DummyLogin struct {
	br *bridgev2.Bridge

	Config Config
	User   *bridgev2.User
	FlowID string
	DAWID  string
}

var crockfordBase32 = base32.NewEncoding("0123456789ABCDEFGHJKMNPQRSTVWXYZ").WithPadding(base32.NoPadding)

func (dl *DummyLogin) Start(ctx context.Context) (*bridgev2.LoginStep, error) {
	switch dl.FlowID {
	case "password":
		return &bridgev2.LoginStep{
			Type:         bridgev2.LoginStepTypeUserInput,
			StepID:       "com.beeper.dummy.password",
			Instructions: "",
			UserInputParams: &bridgev2.LoginUserInputParams{
				Fields: []bridgev2.LoginInputDataField{
					{
						Type: bridgev2.LoginInputFieldTypeUsername,
						ID:   "username",
						Name: "username, anything goes and it's used as the ID",
					},
					{
						Type: bridgev2.LoginInputFieldTypePassword,
						ID:   "password",
						Name: "password, anything goes",
					},
				},
			},
		}, nil
	case "cookies":
		return &bridgev2.LoginStep{
			Type:         bridgev2.LoginStepTypeCookies,
			StepID:       "com.beeper.dummy.cookies",
			Instructions: "",
			CookiesParams: &bridgev2.LoginCookiesParams{
				URL: "https://random.mau.fi/dummy/pages/cookies.html",
				Fields: []bridgev2.LoginCookieField{{
					ID:       "username",
					Required: true,
					Sources: []bridgev2.LoginCookieFieldSource{{
						Type:         bridgev2.LoginCookieTypeCookie,
						Name:         "username",
						CookieDomain: "random.mau.fi",
					}},
				}, {
					ID:       "password",
					Required: true,
					Sources: []bridgev2.LoginCookieFieldSource{{
						Type:         bridgev2.LoginCookieTypeCookie,
						Name:         "password",
						CookieDomain: "random.mau.fi",
					}},
				}},
			},
		}, nil
	case "localstorage":
		return &bridgev2.LoginStep{
			Type:         bridgev2.LoginStepTypeCookies,
			StepID:       "com.beeper.dummy.localstorage",
			Instructions: "",
			CookiesParams: &bridgev2.LoginCookiesParams{
				URL: "https://random.mau.fi/dummy/pages/localstorage.html",
				Fields: []bridgev2.LoginCookieField{{
					ID:       "username",
					Required: true,
					Sources: []bridgev2.LoginCookieFieldSource{{
						Type: bridgev2.LoginCookieTypeLocalStorage,
						Name: "username",
					}},
				}, {
					ID:       "password",
					Required: true,
					Sources: []bridgev2.LoginCookieFieldSource{{
						Type: bridgev2.LoginCookieTypeLocalStorage,
						Name: "password",
					}},
				}},
			},
		}, nil
	case "displayandwait":
		dl.DAWID = randomCode()
		return &bridgev2.LoginStep{
			Type:         bridgev2.LoginStepTypeDisplayAndWait,
			StepID:       "com.beeper.dummy.displayandwait",
			Instructions: "Enter the code on https://random.mau.fi/dummy/pages/daw_submit.html",
			DisplayAndWaitParams: &bridgev2.LoginDisplayAndWaitParams{
				Type: bridgev2.LoginDisplayTypeCode,
				Data: dl.DAWID,
			},
		}, nil
	case "webauthn":
		return &bridgev2.LoginStep{
			Type:   bridgev2.LoginStepTypeWebAuthn,
			StepID: "com.beeper.dummy.webauthn",
			WebAuthnParams: &bridgev2.LoginWebAuthnParams{
				URL: "https://web.whatsapp.com",
				PublicKey: json.RawMessage(`{
					"challenge": "DPgmHAR2lKUoayChDUcaUfoNu5U32XhZ0Vq5z5x2yfc",
					"timeout": 600000,
					"rpId": "whatsapp.com",
					"allowCredentials": [],
					"userVerification": "required",
					"extensions": {
						"uvm": true
					}
				}`),
			},
		}, nil
	default:
		return nil, fmt.Errorf("unknown flow ID %q", dl.FlowID)
	}
}

func randomCode() string {
	randomStr := crockfordBase32.EncodeToString(random.Bytes(5))
	return fmt.Sprintf("%s-%s", randomStr[:4], randomStr[4:])
}

func (dl *DummyLogin) SubmitCookies(ctx context.Context, input map[string]string) (*bridgev2.LoginStep, error) {
	return dl.SubmitUserInput(ctx, input)
}

func (dl *DummyLogin) Wait(ctx context.Context) (*bridgev2.LoginStep, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://random.mau.fi/dummy/api/daw_wait/"+dl.DAWID, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code %d", resp.StatusCode)
	}
	var input map[string]string
	err = json.NewDecoder(resp.Body).Decode(&input)
	if err != nil {
		return nil, err
	}
	return dl.SubmitUserInput(ctx, input)
}

func (dl *DummyLogin) SubmitUserInput(ctx context.Context, input map[string]string) (*bridgev2.LoginStep, error) {
	if input["password"] == "incorrectpassword" {
		return nil, fmt.Errorf("incorrect password")
	}
	username := input["username"]
	if username == "" {
		// The login form advertises "anything goes and it's used as the ID", so an empty
		// username must keep working. Persisting a UserLogin with an empty ID is unsafe though:
		// with split portals enabled, an empty receiver makes GetPortalByKey fail and crashes
		// the host app on every startup (DROID-77263). Generate a random ID instead so the login
		// ID can never be empty.
		username = "dummy-" + strings.ToLower(random.String(12))
	}
	login, err := dl.User.NewLogin(ctx, &database.UserLogin{
		ID:         networkid.UserLoginID(username),
		RemoteName: input["password"],
		RemoteProfile: status.RemoteProfile{
			Name:  input["password"],
			Email: "dummy-email-" + random.String(6) + "@beepbeep",
		},
	}, &bridgev2.NewLoginParams{})
	if err != nil {
		return nil, err
	}

	go func() {
		state := status.BridgeState{
			UserID:     login.UserMXID,
			RemoteName: login.RemoteName,
			StateEvent: status.StateConnected,
			Timestamp:  jsontime.UnixNow(),
		}
		login.BridgeState.Send(state)
	}()

	return &bridgev2.LoginStep{
		Type:         bridgev2.LoginStepTypeComplete,
		StepID:       "com.beeper.dummy.complete",
		Instructions: "Successfully logged in with whatever you provided",
		CompleteParams: &bridgev2.LoginCompleteParams{
			UserLoginID: login.ID,
			UserLogin:   login,
		},
	}, nil
}

type WebAuthnResponse struct {
	ID       string                     `json:"id"`
	RawID    jsonbytes.UnpaddedURLBytes `json:"rawId"`
	Type     string                     `json:"type"`
	Response WebAuthnResponseData       `json:"response"`
}

type WebAuthnResponseData struct {
	ClientDataJSON    jsonbytes.UnpaddedURLBytes  `json:"clientDataJSON"`
	AuthenticatorData jsonbytes.UnpaddedURLBytes  `json:"authenticatorData"`
	Signature         jsonbytes.UnpaddedURLBytes  `json:"signature"`
	UserHandle        *jsonbytes.UnpaddedURLBytes `json:"userHandle"`
}

func (dl *DummyLogin) SubmitWebAuthnResponse(ctx context.Context, response json.RawMessage) (*bridgev2.LoginStep, error) {
	var respStruct WebAuthnResponse
	err := json.Unmarshal(response, &respStruct)
	if err != nil {
		return nil, err
	} else if respStruct.Response.UserHandle == nil {
		return nil, fmt.Errorf("userHandle is nil in WebAuthn response")
	}
	return dl.SubmitUserInput(ctx, map[string]string{
		"username": base64.RawURLEncoding.EncodeToString(*respStruct.Response.UserHandle),
		"password": base32.StdEncoding.EncodeToString(respStruct.Response.Signature),
	})
}

func (dl *DummyLogin) Cancel() {}

const (
	ChallengeFlavorInput   = "input"   // user_input step with a captcha_code field
	ChallengeFlavorCookies = "cookies" // cookies step extracting cookies from a webview
	ChallengeFlavorSpecial = "special" // cookies step with only a special field filled by ExtractJS
	ChallengeFlavorHidden  = "hidden"  // like special, but the webview is never shown
)

func IsValidChallengeFlavor(flavor string) bool {
	switch flavor {
	case ChallengeFlavorInput, ChallengeFlavorCookies, ChallengeFlavorSpecial, ChallengeFlavorHidden:
		return true
	}
	return false
}

type DummyChallenge struct {
	client *DummyClient
}

var _ bridgev2.LoginProcessUserInput = (*DummyChallenge)(nil)
var _ bridgev2.LoginProcessCookies = (*DummyChallenge)(nil)

const challengeExtractJS = `new Promise(resolve => setTimeout(
	() => resolve({dummy_token: "dummy-" + Math.random().toString(36).slice(2)}),
	5000,
))`

func (dc *DummyChallenge) Start(ctx context.Context) (*bridgev2.LoginStep, error) {
	switch dc.client.challengeFlavor() {
	case ChallengeFlavorCookies:
		return &bridgev2.LoginStep{
			Type:         bridgev2.LoginStepTypeCookies,
			StepID:       "com.beeper.dummy.challenge.cookies",
			Instructions: "Dummy cookie challenge: submit the form to clear it.",
			CookiesParams: &bridgev2.LoginCookiesParams{
				URL: "https://random.mau.fi/dummy/pages/cookies.html",
				Fields: []bridgev2.LoginCookieField{{
					ID:       "username",
					Required: true,
					Sources: []bridgev2.LoginCookieFieldSource{{
						Type:         bridgev2.LoginCookieTypeCookie,
						Name:         "username",
						CookieDomain: "random.mau.fi",
					}},
				}, {
					ID:       "password",
					Required: true,
					Sources: []bridgev2.LoginCookieFieldSource{{
						Type:         bridgev2.LoginCookieTypeCookie,
						Name:         "password",
						CookieDomain: "random.mau.fi",
					}},
				}},
			},
		}, nil
	case ChallengeFlavorSpecial, ChallengeFlavorHidden:
		return &bridgev2.LoginStep{
			Type:         bridgev2.LoginStepTypeCookies,
			StepID:       "com.beeper.dummy.challenge.special",
			Instructions: "Dummy special challenge: wait a few seconds for the extraction script to resolve.",
			CookiesParams: &bridgev2.LoginCookiesParams{
				URL:       "https://random.mau.fi/dummy/pages/cookies.html",
				ExtractJS: challengeExtractJS,
				Hidden:    dc.client.challengeFlavor() == ChallengeFlavorHidden,
				Fields: []bridgev2.LoginCookieField{{
					ID:       "dummy_token",
					Required: true,
					Sources: []bridgev2.LoginCookieFieldSource{{
						Type: bridgev2.LoginCookieTypeSpecial,
						Name: "com.beeper.dummy.token",
					}},
				}},
			},
		}, nil
	default:
		return &bridgev2.LoginStep{
			Type:         bridgev2.LoginStepTypeUserInput,
			StepID:       "com.beeper.dummy.challenge",
			Instructions: "Dummy challenge: enter anything to clear it.",
			UserInputParams: &bridgev2.LoginUserInputParams{
				Fields: []bridgev2.LoginInputDataField{{
					Type: bridgev2.LoginInputFieldTypeCaptchaCode,
					ID:   "captcha_code",
					Name: "Enter anything to pass the dummy challenge",
				}},
			},
		}, nil
	}
}

func (dc *DummyChallenge) SubmitUserInput(ctx context.Context, input map[string]string) (*bridgev2.LoginStep, error) {
	return dc.clear(input), nil
}

func (dc *DummyChallenge) SubmitCookies(ctx context.Context, input map[string]string) (*bridgev2.LoginStep, error) {
	return dc.clear(input), nil
}

func (dc *DummyChallenge) clear(input map[string]string) *bridgev2.LoginStep {
	login := dc.client.UserLogin
	dc.client.challengePending.Store(false)
	login.BridgeState.Send(status.BridgeState{
		StateEvent: status.StateConnected,
		Timestamp:  jsontime.UnixNow(),
	})
	received := slices.Sorted(maps.Keys(input))
	// complete with the same userloginid so mautrix reuses the existing login
	return &bridgev2.LoginStep{
		Type:         bridgev2.LoginStepTypeComplete,
		StepID:       "com.beeper.dummy.challenge.complete",
		Instructions: fmt.Sprintf("Challenge cleared (received fields: %s)", strings.Join(received, ", ")),
		CompleteParams: &bridgev2.LoginCompleteParams{
			UserLoginID: login.ID,
			UserLogin:   login,
		},
	}
}

func (dc *DummyChallenge) Cancel() {}
