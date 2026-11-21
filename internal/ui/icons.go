package ui

// iconSet holds every glyph the UI draws. Nerd Font icons (https://www.nerdfonts.com)
// live in the Private Use Area: each is one cell wide, but only renders when the
// terminal uses a Nerd Font, so they are off by default (Settings → Иконки Nerd Font).
type iconSet struct {
	prev, play, pause, next string // player buttons, exactly 2 cells each
	shuffle, repeat, lyrics string // prefixes for the player toggles ("" = text only)
	volume                  string
	sections                []string // one per sectionNames entry, 2 cells incl. space
	queue                   string
	playing                 string // marker in front of the playing track
}

var plainIcons = iconSet{
	prev: "|◄", play: "► ", pause: "||", next: "►|",
	sections: []string{"", "", "", "", "", "", ""},
	queue:    "≡ ",
	playing:  "►",
}

// Font Awesome glyphs from the Nerd Fonts set (nf-fa-*), all in the BMP PUA.
var nerdIcons = iconSet{
	prev:    " ", // nf-fa-step_backward
	play:    " ", // nf-fa-play
	pause:   " ", // nf-fa-pause
	next:    " ", // nf-fa-step_forward
	shuffle: " ", // nf-fa-random
	repeat:  " ", // nf-fa-repeat
	lyrics:  " ", // nf-fa-file_text
	volume:  " ", // nf-fa-volume_up
	sections: []string{
		" ", // Моя музыка — nf-fa-music
		" ", // VK Микс — nf-fa-magic
		" ", // Рекомендации — nf-fa-star
		" ", // Популярное — nf-fa-fire
		" ", // Поиск — nf-fa-search
		" ", // Плейлисты — nf-fa-list
		" ", // Настройки — nf-fa-cog
	},
	queue:   " ", // nf-fa-list_ol
	playing: "",  // nf-fa-volume_up
}

func (m *Model) icons() *iconSet {
	if m.cfg.NerdIconsOn() {
		return &nerdIcons
	}
	return &plainIcons
}
