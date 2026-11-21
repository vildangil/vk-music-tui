package vk

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const kateScope = "audio,offline,friends,groups,status,wall"

// BrowserAuthURL is the Kate implicit-flow page; after login VK redirects to blank.html#access_token=...
func BrowserAuthURL() string {
	q := url.Values{
		"client_id":     {Kate.ID},
		"display":       {"page"},
		"redirect_uri":  {"https://oauth.vk.com/blank.html"},
		"scope":         {kateScope},
		"response_type": {"token"},
		"revoke":        {"1"},
		"v":             {APIVersion},
	}
	return "https://oauth.vk.com/authorize?" + q.Encode()
}

// TokenFromInput accepts a raw token or a pasted redirect URL.
func TokenFromInput(s string) (string, int64) {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "#"); i >= 0 {
		s = s[i+1:]
	} else if i := strings.Index(s, "?"); i >= 0 && strings.Contains(s, "access_token=") {
		s = s[i+1:]
	}
	if strings.Contains(s, "access_token=") {
		v, _ := url.ParseQuery(s)
		var uid int64
		fmt.Sscan(v.Get("user_id"), &uid)
		return v.Get("access_token"), uid
	}
	return s, 0
}

// PasswordAuth performs the direct (password) grant as the given app.
type PasswordAuth struct {
	App             App
	DeviceID        string
	Login, Password string
	Code            string
	CaptchaSID      string
	CaptchaKey      string
	ForceSMS        bool

	anon string
}

type AuthResult struct {
	Token  string `json:"access_token"`
	UserID int64  `json:"user_id"`

	Err            string `json:"error"`
	ErrDesc        string `json:"error_description"`
	ErrType        string `json:"error_type"`
	ValidationType string `json:"validation_type"`
	ValidationSID  string `json:"validation_sid"`
	PhoneMask      string `json:"phone_mask"`
	RedirectURI    string `json:"redirect_uri"`
	CaptchaSID     string `json:"captcha_sid"`
	CaptchaImg     string `json:"captcha_img"`
}

func postForm(ctx context.Context, hc *http.Client, u, ua string, q url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(q.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("нет связи с %s: %w", req.URL.Host, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("непонятный ответ %s (HTTP %d)", req.URL.Host, resp.StatusCode)
	}
	return nil
}

// anonymousToken is what the official Android app gets before the password step.
func (a *PasswordAuth) anonymousToken(ctx context.Context, hc *http.Client) string {
	if a.anon != "" || a.App.Name != Android.Name {
		return a.anon
	}
	var r struct {
		Token string `json:"token"`
	}
	q := url.Values{
		"client_id":     {a.App.ID},
		"api_id":        {a.App.ID},
		"client_secret": {a.App.Secret},
		"device_id":     {a.DeviceID},
		"https":         {"1"},
		"lang":          {"ru"},
		"v":             {APIVersion},
	}
	if postForm(ctx, hc, "https://api.vk.ru/oauth/get_anonym_token", a.App.UA, q, &r) == nil {
		a.anon = r.Token
	}
	return a.anon
}

func (a *PasswordAuth) Do(ctx context.Context, hc *http.Client) (*AuthResult, error) {
	q := url.Values{
		"grant_type":    {"password"},
		"client_id":     {a.App.ID},
		"client_secret": {a.App.Secret},
		"username":      {a.Login},
		"password":      {a.Password},
		"2fa_supported": {"1"},
		"v":             {APIVersion},
	}
	if a.App.Name == Android.Name {
		q.Set("scope", "all")
		q.Set("api_id", a.App.ID)
		q.Set("device_id", a.DeviceID)
		q.Set("device_os", "android")
		q.Set("libverify_support", "0")
		q.Set("https", "1")
		q.Set("lang", "ru")
		if t := a.anonymousToken(ctx, hc); t != "" {
			q.Set("anonymous_token", t)
		}
	} else {
		q.Set("scope", kateScope)
	}
	if a.Code != "" {
		q.Set("code", a.Code)
	}
	if a.ForceSMS {
		q.Set("force_sms", "1")
	}
	if a.CaptchaSID != "" {
		q.Set("captcha_sid", a.CaptchaSID)
		q.Set("captcha_key", a.CaptchaKey)
	}
	var r AuthResult
	if err := postForm(ctx, hc, a.App.TokenURL, a.App.UA, q, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// SendSMS asks VK to send a 2FA code by SMS for a validation sid.
func SendSMS(ctx context.Context, app App, deviceID string, sid string) error {
	c := New(app, "", deviceID)
	return c.Call(ctx, "auth.validatePhone", url.Values{"sid": {sid}, "api_id": {app.ID}, "client_id": {app.ID}, "client_secret": {app.Secret}}, nil)
}
