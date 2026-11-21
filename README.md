<p align="center"><img src="packaging/icons/vk-music-tui-logo.png" width="320" alt="VK Music TUI"></p>

<h1 align="center">VK Music TUI</h1>

<p align="center">Лёгкий плеер VK Музыки в терминале, в стиле Spotify.<br>Windows · macOS · Linux · FreeBSD · Termux</p>

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
При первом запуске войди в VK по коду из SMS или уведомления.

## Возможности

- «Моя музыка», VK Микс, рекомендации, популярное, поиск, плейлисты
- Синхронный текст песен (VK, LRCLIB, Genius)
- Эквалайзер, перемешивание, повтор, очередь
- Скачивание в mp3 и вся музыка офлайн
- Несколько аккаунтов
- Мышь, медиа-клавиши и панель громкости Windows/macOS

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
- **Не установлен mpv** — `winget install shinchiro.mpv` / `brew install mpv` / `sudo apt install mpv`.
- **Включён VPN** — добавь в исключения `vk.com`, `vk.ru`, `userapi.com`, `vkuseraudio.net`, `okcdn.ru`.
- **Ошибка входа** — `vktui login`.

## Лицензия

MIT
