package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// "Sign in with Factory": the OAuth device flow the droid CLI uses (WorkOS
// User Management, the CLI's client id), so the access token is what the
// Daemon accepts as `token` in daemon.authenticate (ADR 0005).
const (
	FactoryCliWorkOSClientID = "client_01HNM792M5G5G1A2THWPXKFMXB"
	WorkOSBaseURL            = "https://api.workos.com/user_management"
	deviceGrant              = "urn:ietf:params:oauth:grant-type:device_code"
	refreshMargin            = time.Minute
)

// LoginStatus is where a sign-in stands.
type LoginStatus string

const (
	SignedOut LoginStatus = "signed-out"
	Pending   LoginStatus = "pending"
	SignedIn  LoginStatus = "signed-in"
)

// LoginPending is a device code waiting for the user.
type LoginPending struct {
	UserCode                string
	VerificationURI         string
	VerificationURIComplete string
	ExpiresAt               time.Time
}

// LoginState is who the Host authenticates as. Source is "droi" for a
// sign-in done in the app, "cli" for the droid CLI's own login.
type LoginState struct {
	Status  LoginStatus
	Error   string
	Pending *LoginPending
	Account *Account
	Source  string
}

type storedLogin struct {
	AccessToken  string  `json:"accessToken"`
	RefreshToken string  `json:"refreshToken"`
	Account      Account `json:"account"`
}

// FactoryAuth runs the device flow and keeps its tokens fresh.
type FactoryAuth struct {
	Load     func() string
	Save     func(string)
	OnChange func(LoginState)
	// FactoryAPIBaseURL serves whoami (Factory's own ids); "" skips it.
	FactoryAPIBaseURL string
	HTTP              *http.Client
	WorkOS            string
	ClientID          string

	mu         sync.Mutex
	loaded     bool
	login      *storedLogin
	state      LoginState
	cancelPoll context.CancelFunc
	refreshing chan struct{}
}

func (a *FactoryAuth) init() {
	if a.loaded {
		return
	}
	a.loaded = true
	if a.Load != nil {
		var s storedLogin
		if raw := a.Load(); raw != "" && json.Unmarshal([]byte(raw), &s) == nil && s.AccessToken != "" && s.RefreshToken != "" && s.Account.UserID != "" {
			a.login = &s
		}
	}
	if a.login != nil {
		acc := a.login.Account
		a.state = LoginState{Status: SignedIn, Account: &acc, Source: "droi"}
	} else {
		a.state = LoginState{Status: SignedOut}
	}
}

func (a *FactoryAuth) client() *http.Client {
	if a.HTTP != nil {
		return a.HTTP
	}
	return http.DefaultClient
}

func (a *FactoryAuth) workos() string {
	if a.WorkOS != "" {
		return a.WorkOS
	}
	return WorkOSBaseURL
}

func (a *FactoryAuth) clientID() string {
	if a.ClientID != "" {
		return a.ClientID
	}
	return FactoryCliWorkOSClientID
}

// State returns the current state.
func (a *FactoryAuth) State() LoginState {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.init()
	return a.state
}

func (a *FactoryAuth) setLocked(s LoginState) {
	a.state = s
	if a.OnChange != nil {
		go a.OnChange(s)
	}
}

func (a *FactoryAuth) persistLocked(s *storedLogin) {
	a.login = s
	if a.Save == nil {
		return
	}
	if s == nil {
		a.Save("")
		return
	}
	b, _ := json.Marshal(s)
	a.Save(string(b))
}

func (a *FactoryAuth) post(ctx context.Context, path string, form url.Values) (*http.Response, []byte, error) {
	form.Set("client_id", a.clientID())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.workos()+path, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("content-type", "application/x-www-form-urlencoded")
	res, err := a.client().Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	return res, b, err
}

// SignIn starts the device flow; it returns once there is a code to enter.
func (a *FactoryAuth) SignIn(ctx context.Context) (*LoginPending, error) {
	a.mu.Lock()
	a.init()
	if a.cancelPoll != nil {
		a.cancelPoll()
	}
	a.mu.Unlock()
	res, b, err := a.post(ctx, "/authorize/device", url.Values{})
	if err != nil {
		return nil, err
	}
	if res.StatusCode/100 != 2 {
		return nil, fmt.Errorf("Could not start sign-in (%d).", res.StatusCode)
	}
	var body struct {
		DeviceCode              string `json:"device_code"`
		UserCode                string `json:"user_code"`
		VerificationURI         string `json:"verification_uri"`
		VerificationURIComplete string `json:"verification_uri_complete"`
		ExpiresIn               int    `json:"expires_in"`
		Interval                int    `json:"interval"`
	}
	if err := json.Unmarshal(b, &body); err != nil {
		return nil, err
	}
	p := &LoginPending{
		UserCode:                body.UserCode,
		VerificationURI:         body.VerificationURI,
		VerificationURIComplete: body.VerificationURIComplete,
		ExpiresAt:               time.Now().Add(time.Duration(body.ExpiresIn) * time.Second),
	}
	pollCtx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	a.cancelPoll = cancel
	a.setLocked(LoginState{Status: Pending, Pending: p})
	a.mu.Unlock()
	go a.poll(pollCtx, cancel, body.DeviceCode, max(body.Interval, 1), p.ExpiresAt)
	return p, nil
}

func (a *FactoryAuth) poll(ctx context.Context, cancel context.CancelFunc, deviceCode string, interval int, expires time.Time) {
	fail := func(msg string) {
		a.mu.Lock()
		defer a.mu.Unlock()
		if ctx.Err() != nil {
			return
		}
		a.cancelPoll = nil
		a.setLocked(LoginState{Status: SignedOut, Error: msg})
	}
	defer cancel()
	for time.Now().Before(expires) {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(interval) * time.Second):
		}
		res, b, err := a.post(ctx, "/authenticate", url.Values{"grant_type": {deviceGrant}, "device_code": {deviceCode}})
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			fail("Sign-in failed (" + err.Error() + ").")
			return
		}
		if res.StatusCode/100 == 2 {
			var t struct {
				AccessToken  string `json:"access_token"`
				RefreshToken string `json:"refresh_token"`
			}
			if json.Unmarshal(b, &t) != nil {
				fail("Sign-in failed.")
				return
			}
			acc := a.describe(ctx, t.AccessToken)
			a.mu.Lock()
			if ctx.Err() == nil {
				a.persistLocked(&storedLogin{AccessToken: t.AccessToken, RefreshToken: t.RefreshToken, Account: acc})
				a.cancelPoll = nil
				a.setLocked(LoginState{Status: SignedIn, Account: &acc, Source: "droi"})
			}
			a.mu.Unlock()
			return
		}
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(b, &e)
		switch e.Error {
		case "authorization_pending":
			continue
		case "slow_down":
			interval++
			continue
		case "access_denied":
			fail("Sign-in was denied in the browser.")
		case "expired_token":
			fail("The sign-in code expired.")
		default:
			code := e.Error
			if code == "" {
				code = fmt.Sprint(res.StatusCode)
			}
			fail("Sign-in failed (" + code + ").")
		}
		return
	}
	fail("The sign-in code expired.")
}

// describe prefers Factory's own ids from whoami, which the Daemon compares,
// over the WorkOS ids in the JWT.
func (a *FactoryAuth) describe(ctx context.Context, token string) Account {
	claims := DecodeJWT(token)
	acc := Account{UserID: "unknown"}
	if s, ok := claims["sub"].(string); ok {
		acc.UserID = s
	}
	if s, ok := claims["org_id"].(string); ok {
		acc.OrgID = &s
	}
	if s, ok := claims["email"].(string); ok {
		acc.Email = &s
	}
	if a.FactoryAPIBaseURL == "" {
		return acc
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.FactoryAPIBaseURL+"/api/cli/whoami", nil)
	if err != nil {
		return acc
	}
	req.Header.Set("authorization", "Bearer "+token)
	res, err := a.client().Do(req)
	if err != nil {
		return acc
	}
	defer res.Body.Close()
	var body struct {
		UserID *string `json:"userId"`
		OrgID  *string `json:"orgId"`
	}
	if res.StatusCode/100 == 2 && json.NewDecoder(res.Body).Decode(&body) == nil {
		if body.UserID != nil {
			acc.UserID = *body.UserID
		}
		if body.OrgID != nil {
			acc.OrgID = body.OrgID
		}
	}
	return acc
}

// CancelSignIn stops a pending sign-in.
func (a *FactoryAuth) CancelSignIn() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.init()
	if a.cancelPoll == nil {
		return
	}
	a.cancelPoll()
	a.cancelPoll = nil
	if a.login != nil {
		acc := a.login.Account
		a.setLocked(LoginState{Status: SignedIn, Account: &acc, Source: "droi"})
	} else {
		a.setLocked(LoginState{Status: SignedOut})
	}
}

// SignOut forgets the login.
func (a *FactoryAuth) SignOut() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.init()
	if a.cancelPoll != nil {
		a.cancelPoll()
		a.cancelPoll = nil
	}
	a.persistLocked(nil)
	a.setLocked(LoginState{Status: SignedOut})
}

// AccessToken is a fresh access token, refreshed near expiry; "" when
// signed out.
func (a *FactoryAuth) AccessToken(ctx context.Context) (string, error) {
	for {
		a.mu.Lock()
		a.init()
		cur := a.login
		if cur == nil {
			a.mu.Unlock()
			return "", nil
		}
		if exp := JWTExpiry(cur.AccessToken); !exp.IsZero() && time.Until(exp) > refreshMargin {
			a.mu.Unlock()
			return cur.AccessToken, nil
		}
		if wait := a.refreshing; wait != nil {
			a.mu.Unlock()
			select {
			case <-wait:
				continue
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
		done := make(chan struct{})
		a.refreshing = done
		a.mu.Unlock()
		token, err := a.refresh(ctx, *cur)
		a.mu.Lock()
		a.refreshing = nil
		close(done)
		a.mu.Unlock()
		return token, err
	}
}

var errExpired = errors.New("Your Factory session expired. Sign in again.")

func (a *FactoryAuth) refresh(ctx context.Context, cur storedLogin) (string, error) {
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {cur.RefreshToken}}
		if cur.Account.OrgID != nil {
			form.Set("organization_id", *cur.Account.OrgID)
		}
		res, b, err := a.post(ctx, "/authenticate", form)
		if err == nil && res.StatusCode/100 == 2 {
			var t struct {
				AccessToken  string `json:"access_token"`
				RefreshToken string `json:"refresh_token"`
			}
			if json.Unmarshal(b, &t) == nil {
				a.mu.Lock()
				cur.AccessToken, cur.RefreshToken = t.AccessToken, t.RefreshToken
				a.persistLocked(&cur)
				a.mu.Unlock()
				return t.AccessToken, nil
			}
		}
		// A 4xx other than 429: the refresh token is gone for good.
		if err == nil && res.StatusCode >= 400 && res.StatusCode < 500 && res.StatusCode != 429 {
			a.mu.Lock()
			a.persistLocked(nil)
			a.setLocked(LoginState{Status: SignedOut, Error: errExpired.Error()})
			a.mu.Unlock()
			return "", nil
		}
		lastErr = err
		if attempt < 3 {
			select {
			case <-time.After(time.Duration(500*attempt) * time.Millisecond):
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
	}
	return "", lastErr
}
