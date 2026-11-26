package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"vktui/internal/config"
	"vktui/internal/player"
)

// listEngines prints the supported audio engines and which ones are installed.
func listEngines(cfg *config.Config) {
	av := player.Available()
	want := cfg.Engine
	if want == "" {
		want = "auto"
	}
	cur, _, err := chooseEngine(cfg)
	fmt.Println("Звуковые движки (выбор: vktui engine <имя>, сейчас:", want+")")
	fmt.Println()
	for _, e := range player.Engines {
		mark := "  "
		if err == nil && e.Name == cur.Name {
			mark = "* "
		}
		where := "не установлен"
		if p, ok := av[e.Name]; ok {
			where = p
		}
		fmt.Printf("%s%-8s %-16s %s\n", mark, e.Name, e.Title, where)
		fmt.Printf("           %s\n", e.Note)
	}
	if err != nil {
		fmt.Println()
		fmt.Println("Ни один движок не найден. Установи mpv:\n" + mpvHelp())
	}
}

func setEngine(cfg *config.Config, args []string) error {
	if len(args) == 0 {
		listEngines(cfg)
		return nil
	}
	name := strings.ToLower(args[0])
	if name == "auto" {
		cfg.Engine, cfg.EnginePath = "", ""
		fmt.Println("Движок: авто (mpv, если есть)")
		return cfg.Save()
	}
	e, ok := player.EngineByName(name)
	if !ok {
		return fmt.Errorf("неизвестный движок %q (есть: auto, mpv, vlc, ffplay, mplayer)", name)
	}
	path := ""
	if len(args) > 1 {
		path = args[1]
	}
	cfg.Engine = e.Name
	if e.Name == "mpv" {
		if path != "" {
			cfg.MPV = path
		}
	} else {
		cfg.EnginePath = path
	}
	if _, _, err := player.Choose(e.Name, path); err != nil {
		fmt.Printf("⚠ %v — сохраню выбор, но пока буду играть через то, что найдётся\n", err)
	}
	fmt.Println("Движок:", e.Title, "—", e.Note)
	return cfg.Save()
}

// installDeps installs mpv (and with it ffmpeg) with the system's package manager.
func installDeps() error {
	if p, err := player.Find(""); err == nil {
		fmt.Println("mpv уже установлен:", p)
		return nil
	}
	run := func(name string, args ...string) error {
		fmt.Println("$", name, strings.Join(args, " "))
		c := exec.Command(name, args...)
		c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
		return c.Run()
	}
	has := func(n string) bool { _, err := exec.LookPath(n); return err == nil }
	switch runtime.GOOS {
	case "darwin":
		for _, s := range []string{"/usr/local/share/vk-music-tui/install-deps.sh", "/Applications/VK Music TUI.app/Contents/Resources/install-deps.sh"} {
			if _, err := os.Stat(s); err == nil {
				return run("/bin/bash", s)
			}
		}
		for _, b := range []string{"/opt/homebrew/bin/brew", "/usr/local/bin/brew"} {
			if _, err := os.Stat(b); err == nil {
				return run(b, "install", "mpv")
			}
		}
		if _, err := os.Stat("/opt/local/bin/port"); err == nil {
			return run("sudo", "/opt/local/bin/port", "install", "mpv")
		}
		return errors.New("нет ни Homebrew, ни MacPorts. Переустанови vk-music-tui из .pkg (он скачает mpv сам) или поставь Homebrew: https://brew.sh")
	case "windows":
		return run("winget", "install", "-e", "--id", "shinchiro.mpv")
	default:
		switch {
		case has("apt-get"):
			return run("sudo", "apt-get", "install", "-y", "mpv")
		case has("dnf"):
			return run("sudo", "dnf", "install", "-y", "mpv")
		case has("pacman"):
			return run("sudo", "pacman", "-S", "--needed", "--noconfirm", "mpv")
		case has("zypper"):
			return run("sudo", "zypper", "install", "-y", "mpv")
		case has("apk"):
			return run("sudo", "apk", "add", "mpv")
		case has("xbps-install"):
			return run("sudo", "xbps-install", "-y", "mpv")
		case has("pkg"):
			return run("sudo", "pkg", "install", "-y", "mpv")
		}
		return errors.New("не знаю, каким менеджером пакетов ставить. Установи mpv вручную:\n" + mpvHelp())
	}
}
