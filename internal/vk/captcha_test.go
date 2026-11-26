package vk

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCaptchaAnswerSentOnce(t *testing.T) {
	var keys []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		keys = append(keys, r.Form.Get("captcha_sid")+"/"+r.Form.Get("captcha_key"))
		if r.Form.Get("captcha_key") != "abc" {
			w.Write([]byte(`{"error":{"error_code":14,"error_msg":"Captcha needed","captcha_sid":"77","captcha_img":"https://api.vk.ru/captcha.php?sid=77"}}`))
			return
		}
		w.Write([]byte(`{"response":{"sid":"S1","flow_names":["otp"],"next_step":{"verification_method":"sms"}}}`))
	}))
	defer srv.Close()
	t.Setenv("VKTUI_API_BASE", srv.URL+"/")

	l := NewCodeLogin("dev", "+70000000000")
	l.anon = "anon" // skip the anonymous-token request
	_, err := l.Start(context.Background())
	var e *APIError
	if !errors.As(err, &e) || !e.IsCaptcha() || e.CaptchaSID != "77" || e.CaptchaImg == "" {
		t.Fatalf("want captcha error, got %v", err)
	}
	l.SetCaptcha(e.CaptchaSID, "abc")
	m, err := l.Start(context.Background())
	if err != nil || m != "sms" {
		t.Fatalf("after captcha: %q %v", m, err)
	}
	// the answer must not stick to later calls
	_, _ = l.Start(context.Background())
	want := []string{"/", "77/abc", "/"}
	if len(keys) != 3 || keys[0] != want[0] || keys[1] != want[1] || keys[2] != want[2] {
		t.Fatalf("captcha params per call: %q", keys)
	}
}
