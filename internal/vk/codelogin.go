package vk

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// authVersion is what the official-app login flow (as in Music-M) uses.
const authVersion = "5.126"

// CodeLogin implements the official VK Android app's passwordless login:
// anonymous token → auth.validateAccount → ecosystem.sendOtp* → ecosystem.checkOtp → oauth/token.
type CodeLogin struct {
	App      App
	DeviceID string
	Login    string

	hc    *http.Client
	anon  string
	sid   string
	Info  string // where the code was sent (masked phone etc.)
	Len   int    // expected code length
	Next  string // verification method VK suggested
	Flows []string

	// Extra is merged into the oauth/token request (2FA code, captcha answer).
	Extra url.Values

	captcha url.Values // answer for the next API call only
}

// SetCaptcha sends the person's captcha answer with the next call.
// key may be empty when the captcha was passed on VK's page instead.
func (l *CodeLogin) SetCaptcha(sid, key string) {
	l.captcha = url.Values{}
	if sid != "" {
		l.captcha.Set("captcha_sid", sid)
	}
	if key != "" {
		l.captcha.Set("captcha_key", key)
	}
}

func NewCodeLogin(deviceID, login string) *CodeLogin {
	return &CodeLogin{App: Android, DeviceID: deviceID, Login: login, hc: &http.Client{Timeout: 25 * time.Second}}
}

func (l *CodeLogin) client() *Client {
	c := New(l.App, l.anon, l.DeviceID)
	c.HTTP = l.hc
	c.Version = authVersion
	if len(l.captcha) > 0 && l.captcha.Get("captcha_key") != "" {
		c.Extra, l.captcha = l.captcha, nil
	} else {
		l.captcha = nil
	}
	return c
}

func (l *CodeLogin) ensureAnon(ctx context.Context) error {
	if l.anon != "" {
		return nil
	}
	var r struct {
		Token string `json:"token"`
		Err   string `json:"error"`
		Desc  string `json:"error_description"`
	}
	q := url.Values{
		"client_id": {l.App.ID}, "api_id": {l.App.ID}, "client_secret": {l.App.Secret},
		"device_id": {l.DeviceID}, "https": {"1"}, "lang": {"ru"}, "v": {authVersion},
	}
	if err := postForm(ctx, l.hc, "https://api.vk.ru/oauth/get_anonym_token", l.App.UA, q, &r); err != nil {
		return err
	}
	if r.Token == "" {
		return fmt.Errorf("VK не выдал анонимный токен: %s %s", r.Err, r.Desc)
	}
	l.anon = r.Token
	return nil
}

// Start validates the login and returns the verification method VK wants ("sms", "push", "password", …).
func (l *CodeLogin) Start(ctx context.Context) (string, error) {
	if err := l.ensureAnon(ctx); err != nil {
		return "", err
	}
	var r struct {
		Sid       string   `json:"sid"`
		FlowName  string   `json:"flow_name"`
		FlowNames []string `json:"flow_names"`
		NextStep  *struct {
			VerificationMethod string `json:"verification_method"`
		} `json:"next_step"`
	}
	err := l.client().Call(ctx, "auth.validateAccount", url.Values{
		"login":             {l.Login},
		"force_password":    {"0"},
		"supported_ways":    {"push,sms,callreset,reserve_code,codegen,email"},
		"flow_type":         {"auth_without_password"},
		"api_id":            {l.App.ID},
		"passkey_supported": {"0"},
	}, &r)
	if err != nil {
		return "", err
	}
	l.sid = r.Sid
	l.Flows = r.FlowNames
	l.Next = "password"
	if r.NextStep != nil && r.NextStep.VerificationMethod != "" {
		l.Next = r.NextStep.VerificationMethod
	}
	return l.Next, nil
}

// Methods lists the available verification methods (best first).
func (l *CodeLogin) Methods(ctx context.Context) []string {
	var raw json.RawMessage
	if err := l.client().Call(ctx, "ecosystem.getVerificationMethods", url.Values{"sid": {l.sid}, "api_id": {l.App.ID}}, &raw); err != nil {
		return nil
	}
	var r struct {
		Methods []struct {
			Name string `json:"name"`
		} `json:"methods"`
	}
	_ = json.Unmarshal(raw, &r)
	var out []string
	for _, m := range r.Methods {
		out = append(out, m.Name)
	}
	return out
}

// Send asks VK to deliver a one-time code by the given method.
func (l *CodeLogin) Send(ctx context.Context, method string) error {
	l.Len = 6
	var m string
	switch method {
	case "sms":
		m = "ecosystem.sendOtpSms"
	case "callreset":
		m = "ecosystem.sendOtpCallReset"
	case "push":
		m = "ecosystem.sendOtpPush"
	default: // email, codegen, reserve_code: code already exists, nothing to send
		return nil
	}
	var r struct {
		Sid        string `json:"sid"`
		Info       string `json:"info"`
		CodeLength int    `json:"code_length"`
	}
	if err := l.client().Call(ctx, m, url.Values{"sid": {l.sid}, "api_id": {l.App.ID}}, &r); err != nil {
		return err
	}
	if r.Sid != "" {
		l.sid = r.Sid
	}
	l.Info = r.Info
	if r.CodeLength > 0 {
		l.Len = r.CodeLength
	}
	return nil
}

// Check verifies the code. canSkipPassword=false means a password is still needed.
func (l *CodeLogin) Check(ctx context.Context, method, code string) (canSkipPassword bool, err error) {
	var r struct {
		Sid             string `json:"sid"`
		ProfileExist    bool   `json:"profile_exist"`
		CanSkipPassword bool   `json:"can_skip_password"`
	}
	err = l.client().Call(ctx, "ecosystem.checkOtp", url.Values{
		"sid": {l.sid}, "api_id": {l.App.ID}, "verification_method": {method}, "code": {strings.TrimSpace(code)},
	}, &r)
	if err != nil {
		return false, err
	}
	if r.Sid != "" {
		l.sid = r.Sid
	}
	if !r.ProfileExist {
		return false, fmt.Errorf("аккаунт VK с таким логином не найден")
	}
	return r.CanSkipPassword, nil
}

// Token finishes the login. password may be empty when VK allowed skipping it.
// usedCode=false means the plain password flow (no OTP step happened).
func (l *CodeLogin) Token(ctx context.Context, password string, usedCode bool) (*AuthResult, error) {
	if err := l.ensureAnon(ctx); err != nil {
		return nil, err
	}
	grant := "password"
	flow := "tg_flow"
	switch {
	case usedCode && password == "":
		grant, flow = "without_password", "auth_without_password"
	case usedCode:
		grant = "phone_confirmation_sid"
	}
	q := url.Values{
		"grant_type":        {grant},
		"client_id":         {l.App.ID},
		"client_secret":     {l.App.Secret},
		"api_id":            {l.App.ID},
		"username":          {l.Login},
		"scope":             {"all"},
		"device_id":         {l.DeviceID},
		"device_os":         {"android"},
		"libverify_support": {"0"},
		"supported_ways":    {"push,email"},
		"flow_type":         {flow},
		"2fa_supported":     {"1"},
		"vk_connect_auth":   {"1"},
		"https":             {"1"},
		"lang":              {"ru"},
		"v":                 {authVersion},
		"anonymous_token":   {l.anon},
	}
	if l.sid != "" {
		q.Set("sid", l.sid)
	}
	if password != "" {
		q.Set("password", password)
	}
	for k, v := range l.Extra {
		q[k] = v
	}
	var r AuthResult
	if err := postForm(ctx, l.hc, l.App.TokenURL, l.App.UA, q, &r); err != nil {
		return nil, err
	}
	return &r, nil
}
