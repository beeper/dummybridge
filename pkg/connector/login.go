package connector

import (
	"context"
	"encoding/base32"
	"encoding/json"
	"fmt"
	"net/http"

	"go.mau.fi/util/random"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
)

type DummyLogin struct {
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
	ul, err := dl.User.NewLogin(ctx, &database.UserLogin{
		ID: networkid.UserLoginID(input["username"]),
	}, &bridgev2.NewLoginParams{})
	if err != nil {
		return nil, err
	}
	return &bridgev2.LoginStep{
		Type:         bridgev2.LoginStepTypeComplete,
		StepID:       "com.beeper.dummy.complete",
		Instructions: "Successfully logged in with whatever you provided",
		CompleteParams: &bridgev2.LoginCompleteParams{
			UserLoginID: ul.ID,
			UserLogin:   ul,
		},
	}, nil
}

func (dl *DummyLogin) Cancel() {}
