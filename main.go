package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"

	"vktui/internal/config"
	"vktui/internal/player"
	"vktui/internal/ui"
	"vktui/internal/vk"
)

var version = "dev"

const usage = `vktui — консольный плеер VK Музыки

Использование:
  vktui            запустить плеер (при первом запуске попросит войти)
  vktui login      войти (добавить ещё один аккаунт)
  vktui accounts   список аккаунтов
  vktui switch N   переключиться на аккаунт N
  vktui logout     выйти из текущего аккаунта
  vktui doctor     проверить mpv, сеть и токен
  vktui version    показать версию

Переменные окружения:
  VKTUI_MPV         путь к mpv, если он не в PATH
  VKTUI_CONFIG_DIR  папка конфига
  HTTPS_PROXY       прокси для запросов к VK API
`

var in = bufio.NewReader(os.Stdin)

func main() {
	if os.Getenv("GOGC") == "" {
		debug.SetGCPercent(25) // keep RSS small; the app allocates very little
	}
	cmd := ""
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	cfg := config.Load()
	var err error
	switch cmd {
	case "":
		err = run(cfg)
	case "login":
		err = login(cfg)
	case "logout":
		cfg.RemoveActive()
		err = cfg.Save()
		if err == nil {
			if i := cfg.ActiveIndex(); i >= 0 {
				fmt.Println("Вышел. Теперь активен:", cfg.Accounts[i].Label())
			} else {
				fmt.Println("Вышел из аккаунта. Аккаунтов больше нет — `vktui login`, чтобы войти.")
			}
		}
	case "accounts":
		listAccounts(cfg)
	case "switch":
		err = switchAccount(cfg, os.Args[2:])
	case "doctor":
		doctor(cfg)
	case "version", "-v", "--version":
		fmt.Println("vktui", version, runtime.GOOS+"/"+runtime.GOARCH)
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Print(usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "Ошибка:", err)
		os.Exit(1)
	}
}

func mpvHelp() string {
	switch runtime.GOOS {
	case "windows":
		return "  winget install shinchiro.mpv\n  (или: scoop install mpv, или положи mpv.exe рядом с vktui.exe)"
	case "darwin":
		return "  brew install mpv"
	default:
		return "  Debian/Ubuntu: sudo apt install mpv\n  Arch:          sudo pacman -S mpv\n  Fedora:        sudo dnf install mpv\n  Alpine:        sudo apk add mpv"
	}
}

func run(cfg *config.Config) error {
	mpvPath, err := player.Find(cfg.MPV)
	if err != nil {
		return fmt.Errorf("для звука нужен mpv. Установи его:\n%s\nили укажи путь в переменной VKTUI_MPV", mpvHelp())
	}
	if cfg.Token == "" {
		if err := login(cfg); err != nil {
			return err
		}
	}
	pl, err := player.Start(mpvPath, cfg.Volume)
	if err != nil {
		return err
	}
	defer pl.Close()

	for {
		m := ui.New(newClient(cfg), pl, cfg)
		p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
		final, err := p.Run()
		if err != nil {
			return err
		}
		fm, ok := final.(ui.Model)
		if ok {
			fm.Shutdown()
		}
		if !ok || !fm.WantLogin() {
			return nil
		}
		// "Добавить аккаунт" in the settings: sign in here, then back to the player
		pl.Stop()
		fmt.Println()
		if err := login(cfg); err != nil {
			fmt.Println("Ошибка:", err)
			prompt("Enter — вернуться в плеер")
		}
	}
}

func listAccounts(cfg *config.Config) {
	act := cfg.ActiveIndex()
	if len(cfg.Accounts) == 0 {
		fmt.Println("Аккаунтов нет — `vktui login`, чтобы войти.")
		return
	}
	for i, a := range cfg.Accounts {
		mark := "  "
		if i == act {
			mark = "* "
		}
		fmt.Printf("%s%d) %s\n", mark, i+1, a.Label())
	}
}

func switchAccount(cfg *config.Config, args []string) error {
	var n int
	if len(args) == 0 {
		listAccounts(cfg)
		fmt.Sscan(prompt("Номер аккаунта: "), &n)
	} else {
		fmt.Sscan(args[0], &n)
	}
	if n < 1 || n > len(cfg.Accounts) {
		return fmt.Errorf("нет аккаунта с номером %d", n)
	}
	cfg.Use(cfg.Accounts[n-1])
	if err := cfg.Save(); err != nil {
		return err
	}
	fmt.Println("Активен:", cfg.Accounts[n-1].Label())
	return nil
}

// ---------- login ----------

func prompt(s string) string {
	fmt.Print(s)
	line, _ := in.ReadString('\n')
	return strings.TrimSpace(line)
}

func promptSecret(s string) string {
	fmt.Print(s)
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		b, err := term.ReadPassword(fd)
		fmt.Println()
		if err == nil {
			return strings.TrimSpace(string(b))
		}
	}
	line, _ := in.ReadString('\n')
	return strings.TrimSpace(line)
}

func openBrowser(url string) {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		c = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		c = exec.Command("open", url)
	default:
		c = exec.Command("xdg-open", url)
	}
	_ = c.Start()
}

func newClient(cfg *config.Config) *vk.Client {
	return vk.New(vk.AppByName(cfg.App), cfg.Token, cfg.DeviceID)
}

func login(cfg *config.Config) error {
	if cfg.DeviceID == "" {
		cfg.DeviceID = vk.NewDeviceID()
	}
	fmt.Println("Вход в VK")
	fmt.Println()
	fmt.Println("  1) По коду из SMS / уведомления VK (рекомендуется)")
	fmt.Println("  2) Логин и пароль")
	fmt.Println("  3) Через браузер (Kate Mobile, старый способ — VK может не дать доступ к музыке)")
	fmt.Println("  4) Вставить готовый токен")
	fmt.Println()
	var token string
	var uid int64
	var err error
	app := vk.Android
	switch prompt("Выбор [1]: ") {
	case "", "1":
		token, uid, err = codeLogin(cfg, true)
	case "2":
		token, uid, err = codeLogin(cfg, false)
	case "3":
		app = vk.Kate
		token, uid = browserLogin()
	case "4":
		if strings.HasPrefix(strings.ToLower(prompt("Токен от Kate Mobile? [y/N]: ")), "y") {
			app = vk.Kate
		}
		token, uid = vk.TokenFromInput(promptSecret("Токен (ввод скрыт): "))
	default:
		return errors.New("неизвестный вариант")
	}
	if err != nil {
		return err
	}
	if token == "" {
		return errors.New("токен не получен")
	}

	fmt.Println("Проверяю токен…")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := vk.New(app, token, cfg.DeviceID)
	var u vk.User
	for attempt := 0; attempt < 3; attempt++ {
		u, err = c.Me(ctx)
		if !isFlood(err) {
			break
		}
		wait := time.Duration(2<<attempt) * time.Second
		fmt.Printf("VK просит подождать (flood control), повтор через %v…\n", wait)
		time.Sleep(wait)
	}
	if err != nil && !isFlood(err) {
		return fmt.Errorf("токен не работает: %w", err)
	}
	if uid == 0 {
		uid = u.ID
	}
	cfg.AddAccount(config.Account{Name: strings.TrimSpace(u.FirstName + " " + u.LastName), UserID: uid, Token: token, App: app.Name, DeviceID: cfg.DeviceID})
	if err := cfg.Save(); err != nil {
		return err
	}
	if err != nil {
		fmt.Println("⚠ VK пока отвечает «Flood control», но токен сохранён. Подожди пару минут и запусти `vktui doctor`.")
		return nil
	}
	if tr, _, err := c.Audio(ctx, 0, 0, "", 0, 3); err != nil {
		fmt.Println("⚠ Аудио недоступно с этим токеном:", err)
	} else if len(tr) > 0 && !tr[0].Playable() {
		fmt.Println("⚠ VK отдаёт заглушки вместо треков.")
	} else {
		fmt.Println("Музыка доступна ✓")
	}
	fmt.Printf("Готово! Привет, %s %s. Токен сохранён в %s\n", u.FirstName, u.LastName, config.Path())
	return nil
}

var methodNames = map[string]string{
	"sms":          "SMS",
	"push":         "уведомление в приложении VK",
	"callreset":    "звонок (код — последние цифры номера)",
	"email":        "код из письма на почту",
	"codegen":      "код из приложения-генератора",
	"reserve_code": "резервный код",
	"password":     "пароль",
}

func methodName(m string) string {
	if n, ok := methodNames[m]; ok {
		return n
	}
	return m
}

// codeLogin runs the official-app login. withCode=false skips straight to the password.
func codeLogin(cfg *config.Config, withCode bool) (string, int64, error) {
	l := vk.NewCodeLogin(cfg.DeviceID, prompt("Телефон или email: "))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	if !withCode {
		return finishToken(ctx, l, promptSecret("Пароль (ввод скрыт): "), false)
	}

	method, err := l.Start(ctx)
	if err != nil {
		return "", 0, fmt.Errorf("VK не принял логин: %w", err)
	}
	if method == "password" {
		fmt.Println("VK просит пароль для этого аккаунта.")
		return finishToken(ctx, l, promptSecret("Пароль (ввод скрыт): "), false)
	}

	for {
		if err := l.Send(ctx, method); err != nil {
			fmt.Println("Не удалось отправить код:", err)
		}
		where := methodName(method)
		if l.Info != "" {
			where += " (" + l.Info + ")"
		}
		fmt.Printf("Код отправлен: %s\n", where)
		code := prompt("Введи код (или «другой» — выбрать другой способ): ")
		if strings.EqualFold(code, "другой") || strings.EqualFold(code, "other") {
			ms := l.Methods(ctx)
			if len(ms) == 0 {
				ms = []string{"sms", "push", "callreset", "email", "password"}
			}
			for i, m := range ms {
				fmt.Printf("  %d) %s\n", i+1, methodName(m))
			}
			var n int
			fmt.Sscan(prompt("Номер: "), &n)
			if n >= 1 && n <= len(ms) {
				method = ms[n-1]
			}
			if method == "password" {
				return finishToken(ctx, l, promptSecret("Пароль (ввод скрыт): "), false)
			}
			continue
		}
		skip, err := l.Check(ctx, method, code)
		if err != nil {
			fmt.Println("Код не подошёл:", err)
			continue
		}
		pass := ""
		if !skip {
			pass = promptSecret("VK просит ещё и пароль (ввод скрыт): ")
		}
		return finishToken(ctx, l, pass, true)
	}
}

func finishToken(ctx context.Context, l *vk.CodeLogin, password string, usedCode bool) (string, int64, error) {
	l.Extra = url.Values{}
	for attempt := 0; attempt < 6; attempt++ {
		r, err := l.Token(ctx, password, usedCode)
		if err != nil {
			return "", 0, err
		}
		if r.Token != "" {
			return r.Token, r.UserID, nil
		}
		switch r.Err {
		case "need_validation":
			if r.ValidationSID != "" {
				l.Extra.Set("validation_sid", r.ValidationSID)
			}
			where := "из SMS или уведомления VK"
			if r.ValidationType == "2fa_app" {
				where = "из приложения-аутентификатора / уведомления VK"
			} else if r.PhoneMask != "" {
				where = "из SMS на " + r.PhoneMask
			}
			l.Extra.Set("code", prompt("Код двухфакторной защиты ("+where+"): "))
		case "need_captcha":
			fmt.Println("VK просит капчу. Открой картинку и введи текст:")
			fmt.Println("  " + r.CaptchaImg)
			openBrowser(r.CaptchaImg)
			l.Extra.Set("captcha_sid", r.CaptchaSID)
			l.Extra.Set("captcha_key", prompt("Капча: "))
		case "invalid_client", "invalid_request":
			if strings.Contains(strings.ToLower(r.ErrDesc), "code") {
				l.Extra.Set("code", prompt("Неверный код, ещё раз: "))
				continue
			}
			return "", 0, fmt.Errorf("VK отклонил вход: %s %s", r.ErrDesc, r.ErrType)
		default:
			return "", 0, fmt.Errorf("VK: %s %s %s", r.Err, r.ErrType, r.ErrDesc)
		}
	}
	return "", 0, errors.New("слишком много попыток")
}

func browserLogin() (string, int64) {
	u := vk.BrowserAuthURL()
	fmt.Println()
	fmt.Println("Сейчас откроется страница входа VK. Если не открылась — скопируй ссылку:")
	fmt.Println()
	fmt.Println("  " + u)
	fmt.Println()
	fmt.Println("После входа браузер попадёт на пустую страницу blank.html.")
	fmt.Println("Скопируй ПОЛНЫЙ адрес из адресной строки и вставь сюда.")
	fmt.Println()
	openBrowser(u)
	return vk.TokenFromInput(promptSecret("Адрес (ввод скрыт): "))
}

// ---------- doctor ----------

func doctor(cfg *config.Config) {
	ok := func(b bool) string {
		if b {
			return "[ OK ]"
		}
		return "[FAIL]"
	}
	fmt.Println("vktui", version, runtime.GOOS+"/"+runtime.GOARCH)
	fmt.Println("Конфиг:", config.Path())
	fmt.Println("Загрузки:", cfg.Downloads())
	fmt.Println("Офлайн:  ", config.OfflineDir())
	fmt.Println("Кэш:     ", config.CacheDir())

	p, err := player.Find(cfg.MPV)
	fmt.Println(ok(err == nil), "mpv:", p)
	if err != nil {
		fmt.Println("       установи mpv:\n" + mpvHelp())
	} else {
		out, _ := exec.Command(p, "--version").Output()
		if l := strings.SplitN(string(out), "\n", 2); len(l) > 0 {
			fmt.Println("       ", strings.TrimSpace(l[0]))
		}
	}

	hc := &http.Client{Timeout: 10 * time.Second}
	for _, h := range []string{"https://api.vk.com", "https://oauth.vk.com"} {
		resp, err := hc.Get(h)
		if err == nil {
			resp.Body.Close()
		}
		fmt.Println(ok(err == nil), "связь с", h, errStr(err))
	}

	if cfg.Token == "" {
		fmt.Println("[FAIL] токена нет — запусти `vktui login`")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c := newClient(cfg)
	fmt.Println("       приложение:", c.App.Name)
	u, err := c.Me(ctx)
	fmt.Println(ok(err == nil), "токен:", u.FirstName, u.LastName, errStr(err))
	if isFlood(err) {
		c2 := newClient(cfg)
		c2.UA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64)"
		u2, err2 := c2.Me(ctx)
		fmt.Println("       тот же запрос с браузерным User-Agent:", u2.FirstName, errStr(err2))
	}
	if err != nil && !isFlood(err) {
		return
	}
	tr, n, err := c.Audio(ctx, 0, 0, "", 0, 1)
	good := err == nil && (len(tr) == 0 || tr[0].Playable())
	fmt.Println(ok(good), "аудио:", n, "треков", errStr(err))
	if len(tr) == 0 || !tr[0].Playable() {
		return
	}
	t := tr[0]
	pu, _ := url.Parse(t.URL)
	kind := "mp3"
	if strings.Contains(pu.Path, ".m3u8") {
		kind = "HLS (m3u8)"
	}
	fmt.Println("       тестовый трек:", t.Artist, "—", t.Title, "| хост:", pu.Host, "| формат:", kind)
	req, _ := http.NewRequest(http.MethodGet, t.URL, nil)
	req.Header.Set("User-Agent", c.App.UA)
	resp, herr := hc.Do(req)
	status := ""
	if herr == nil {
		status = resp.Status
		resp.Body.Close()
		if resp.StatusCode >= 400 {
			herr = errors.New(resp.Status)
		}
	}
	fmt.Println(ok(herr == nil), "скачивание трека:", status, redact(errStr(herr)))
	if ips, err := net.LookupHost(pu.Hostname()); err == nil {
		fmt.Println("       IP сервера с музыкой:", strings.Join(ips, ", "))
	}
	if herr != nil {
		fmt.Println("       VK отдаёт музыку с", pu.Host, "— если включён VPN/прокси, добавь этот домен (или IP выше) в исключения (см. README).")
	}
	if p == "" {
		return
	}
	// Play 3 seconds with no audio output and show what mpv complains about.
	mctx, mcancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer mcancel()
	cmd := exec.CommandContext(mctx, p, "--no-config", "--no-video", "--ytdl=no", "--ao=null", "--length=3",
		"--msg-level=all=warn", "--term-playing-msg=", t.URL)
	out, merr := cmd.CombinedOutput()
	fmt.Println(ok(merr == nil), "воспроизведение через mpv", errStr(merr))
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			fmt.Println("        mpv:", redact(line))
		}
	}
}

// redact keeps only scheme+host of URLs: VK stream links carry signatures and the user's IP.
var urlRe = regexp.MustCompile(`https?://[^/\s"']+[^\s"']*`)

func redact(s string) string {
	return urlRe.ReplaceAllStringFunc(s, func(u string) string {
		if pu, err := url.Parse(u); err == nil {
			return pu.Scheme + "://" + pu.Host + "/…"
		}
		return "…"
	})
}

func errStr(err error) string {
	if err == nil {
		return ""
	}
	return "— " + err.Error()
}

func isFlood(err error) bool {
	var e *vk.APIError
	return errors.As(err, &e) && (e.Code == 9 || e.Code == 6)
}
