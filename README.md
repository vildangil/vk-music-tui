<p align="center"><img src="packaging/icons/vk-music-tui-logo.png" width="320" alt="VK Music TUI"></p>

<h1 align="center">VK Music TUI</h1>

<p align="center">Лёгкий плеер VK Музыки в терминале, в стиле Spotify-tui.<br>Windows · macOS · Linux · FreeBSD · Termux (зачем и как оно работает? я сам хз вопросы к клоду)</p>

## Установка

**Windows** — скачай и запусти [`vk-music-tui-setup-amd64.exe`](https://github.com/vildangil/vk-music-tui/releases/latest/download/vk-music-tui-setup-amd64.exe).

**macOS** — скачай `.pkg` со страницы [Releases](https://github.com/vildangil/vk-music-tui/releases/latest) и открой (правый клик → «Открыть»).

**Linux** — одна команда для любого дистрибутива:
```sh
curl -fsSL https://raw.githubusercontent.com/vildangil/vk-music-tui/main/install/install.sh | sh
```

Все файлы (deb, rpm, Arch, Alpine, ARM, архивы) — в [Releases](https://github.com/vildangil/vk-music-tui/releases/latest).

## Запуск

```sh
vktui
```
При первом запуске войди в VK: по коду из SMS / уведомления, по звонку (вводишь последние цифры номера, с которого позвонили — можно вставить номер целиком) или по паролю.

На macOS плеер также есть в Launchpad и Программах — «VK Music TUI» откроет его в Терминале.

## Возможности

- «Моя музыка», VK Микс, рекомендации, популярное, поиск, плейлисты
- Синхронный текст песен (VK, LRCLIB, Genius)
- Эквалайзер, перемешивание, повтор, очередь
- Скачивание в mp3 и вся музыка офлайн
- Несколько аккаунтов
- 11 цветов темы (настройки, `7`)
- Мышь, медиа-клавиши и панель громкости Windows/macOS, виджет «Сейчас играет» в Linux (MPRIS: GNOME, KDE, playerctl, waybar)
- Несколько звуковых движков: mpv (по умолчанию), VLC, FFmpeg (ffplay), MPlayer

## Звуковой движок

```sh
vktui engines        # что установлено и что выбрано
vktui engine vlc     # выбрать: auto, mpv, vlc, ffplay, mplayer
vktui deps           # поставить mpv (macOS: Homebrew / MacPorts / официальная сборка)
```
Выбрать движок можно и в настройках (`7`). mpv умеет всё; у VLC и MPlayer нет эквалайзера, у ffplay пауза, перемотка и громкость перезапускают поток. Для скачивания нужен mpv или ffmpeg.

## Клавиши

| | |
|---|---|
| `Enter` / двойной клик | играть |
| `Space` | пауза |
| `n` / `p` | следующий / предыдущий |
| `←` `→` | перемотка |
| `+` `-` | громкость |
| `s` / `r` | перемешать / повтор |
| `/` | поиск |
| `t` | текст песни |
| `o` | очередь |
| `d` | скачать трек |
| `1`–`7` | разделы (`7` — настройки) |
| `q` | выход |

Клавиши работают и в русской раскладке.

## Если не работает

```sh
vktui doctor
```
Покажет, что не так. Частые причины:
- **Не установлен mpv** — `vktui deps`, или `winget install shinchiro.mpv` / `brew install mpv` / `sudo apt install mpv`. Можно играть и через VLC / ffplay / MPlayer: `vktui engines`.
- **«Не удалось отправить код»** — VK ограничил попытки: подожди пару минут или выбери другой способ (вход предложит сам).
- **«Unknown method passed» / Flood control с токеном Kate** — VK больше не даёт музыку токенам Kate Mobile, войди по коду: `vktui login`.
- **Включён VPN** — добавь в исключения `vk.com`, `vk.ru`, `userapi.com`, `vkuseraudio.net`, `okcdn.ru`.
- **Ошибка входа** — `vktui login`.

## Лицензия

MIT
