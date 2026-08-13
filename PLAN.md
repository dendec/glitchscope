# PMV — Portable Music Visualizer

> Этот файл сохранён как исторический roadmap ранней версии. Нормативная
> архитектура находится в [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md), а
> актуальные UI-требования — в [docs/UI-PLAN.md](docs/UI-PLAN.md). Разделы
> ниже не следует использовать для восстановления текущих модулей.

Аудиоплеер с MilkDrop-совместимой визуализацией для портативных игровых
консолей (PortMaster: TrimUI Smart Pro, Anbernic и др.).

**pmv** = **P**ortable **M**usic **V**isualizer.

Воспроизведение аудио (MP3/FLAC/WAV/Ogg) + трекерной музыки (MOD/XM/IT/S3M/...)
+ визуализация через projectM (MilkDrop-совместимый движок) поверх OpenGL.

---

## Вдохновение

Проект строится по образу и подобию `../zimlite` (ZIM-ридер для портативок):

- Архитектура Go-приложения с cgo-мостами к C/C++ библиотекам
- Билд-система (Makefile + Dockerfile.arm64 + Dockerfile.windows)
- PortMaster-упаковка
- Обработка ввода (gamepad + keyboard)
- //go:embed статических ассетов

Zimlite лежит в `~/workspace/zimlite`. Смотреть туда за паттернами:
config, i18n, input обработка, Makefile, Dockerfile, Menu/UI, вердоринг C++.

---

## Стек

| Компонент | Технология | Интеграция |
|-----------|-----------|------------|
| Язык | Go 1.25 | cmd/pmv/main.go |
| Окно + ввод | SDL2 (go-sdl2) | renderer/, ui/ |
| OpenGL контекст | через SDL2 | renderer/ (совместно с go-sdl2) |
| Аудио | SoLoud (vendored C++) | cgo-мост в internal/soloud/ |
| Трекеры | libopenmpt (через SoLoud) | системная зависимость, кросс-сборка в Docker |
| Визуализация | projectM (vendored C++) | cgo-мост в internal/projectm/ |
| Пресеты | .milk файлы (100+) | //go:embed в assets/presets/ |
| Билд | Makefile + Docker | копия zimlite |

---

## Структура репозитория

```
pmv/
├── cmd/pmv/main.go              # Точка входа
├── Makefile                       # build/test/lint/dist/deploy
├── Dockerfile.arm64               # ARM64 кросс-сборка для портативок
├── Dockerfile.windows             # Windows кросс-сборка (Zig)
├── go.mod                         # Зависимости: go-sdl2 только
├── config.json                    # Настройки по умолчанию
│
├── internal/
│   ├── config/
│   │   └── config.go              # Config: Load/Save/Get/Set/Provider (как zimlite)
│   │
│   ├── soloud/                    # cgo-мост к SoLoud C API
│   │   ├── soloud.go              # Go-обёртка: Init/Play/Pause/Volume/Seek
│   │   ├── fft.go                 # calcFFT() + getWave() — для projectM
│   │   └── bridge.c/.h            # #include "soloud_c.h" вспомогательный клей
│   │   [вендор: soloud/src/*, soloud/include/*]
│   │
│   ├── projectm/                  # cgo-мост к libprojectM
│   │   ├── projectm.go            # Init/Render/Resize/Preset*
│   │   ├── bridge.h               # extern "C" врапперы projectM C++ API
│   │   ├── bridge.cpp             # C++ → C
│   │   └── dummy.go               # !cgo заглушка (как zimlite)
│   │   [вендор: projectM исходники где-то в дереве или submodule]
│   │
│   ├── player/
│   │   ├── player.go              # Player: открыть/играть/пауза/след/пред
│   │   ├── playlist.go            # Плейлист: shuffle/repeat/queue/next/prev
│   │   └── scanner.go             # Рекурсивный поиск аудиофайлов по маскам
│   │
│   ├── renderer/
│   │   ├── renderer.go            # SDL2 окно, OpenGL контекст, цикл рендера
│   │   └── fft_bridge.go          # Передача FFT/wave данных из SoLoud в projectM
│   │
│   ├── ui/
│   │   ├── ui.go                  # App: главный цикл событий, смена режимов
│   │   ├── input.go               # Input: gamepad + keyboard (паттерн zimlite)
│   │   ├── browser.go             # Файловый браузер (навигация по музыке)
│   │   ├── playlist_ui.go         # Экран плейлиста (B-кнопка → показать/скрыть)
│   │   └── overlay.go             # Now-playing: трек, время, пресет
│   │
│   ├── i18n/                      # i18n как в zimlite: T(key), карты
│   │   ├── i18n.go
│   │   ├── en.go, ru.go
│   │   └── i18n_test.go
│   │
│   └── util/
│       └── string.go
│
├── portmaster/                    # Упаковка для PortMaster
│   ├── Pmv.sh                    # Лаунчер с LD_LIBRARY_PATH
│   └── port.json                  # Метаданные
│
├── scripts/
│   └── pmv.sh                    # Деплой на устройство
│
├── assets/
│   └── presets/                   # 100+ .milk пресетов (//go:embed)
│
└── PLAN.md                        # Этот файл
```

---

## Поток данных

```
[Файлы на SD-карте] → scanner.go → playlist.go → player.go
                                                        ↓
                                              soloud/ (SoLoud C API)
                                              ├── Wav.load() / WavStream
                                              └── Openmpt.load() / loadMem()
                                                        ↓
                                              soLoud через SDL2 backend → динамики
                                                        ↓
                                              getWave() + calcFFT() → FFT
                                                        ↓
                                              projectm/ → Render(FFT) → RGBA
                                                        ↓
                                              renderer/ → SDL2 + OpenGL → экран
                                                        ↓
                                              ui/ input → управление
```

---

## Ключевые решения

### SoLoud вместо miniaudio + libopenmpt по отдельности
SoLoud — один вендор, покрывает WAV/MP3/FLAC/Ogg (встроенные декодеры) +
трекеры через libopenmpt. Даёт calcFFT() и getWave() для projectM.
Полный C API — чистый cgo-мост.

### projectM — вендоренный C++ через cgo
Как LunaSVG в zimlite. extern "C" врапперы, C++ → C, Go вызывает C.
OpenGL 2.1+ достаточно для MilkDrop-совместимости.

### libopenmpt — системная зависимость
SoLoud::Openmpt подгружает libopenmpt.so/.dll динамически.
Для ARM64 кросс-сборка в Dockerfile (как libzim в zimlite).

### Пресеты — вкомпилены //go:embed
100+ .milk файлов в assets/presets/, зазипованы и встроены в бинарь.
На устройстве не нужны внешние пресеты (но можно до-класть).

### Билд — 1:1 копия zimlite
Makefile цели: build, test, lint, dist-arm64, dist-windows, dist-portmaster,
deploy. Dockerfile.arm64 + Dockerfile.windows.

---

## Что НЕ входит в v0.1

- Стриминг/интернет-радио
- ID3-теги и метаданные
- Эквалайзер / DSP
- Запись / захват
- Темы (только тёмная)
- Сетевые функции

---

## Фазы реализации

1. [x] **Скелет** — go.mod, cmd, Makefile, config, пустое SDL2-окно
2. [x] **SoLoud** — вендор, cgo-мост, тестовый проигрыватель WAV/MP3
3. [x] **projectM** — вендор, cgo-мост, рендер тестовой сцены по FFT
4. [x] **Плеер** — Player + Playlist + Scanner, полный цикл
5. [x] **UI** — Браузер, плейлист, оверлей, gamepad, i18n
6. [x] **Билд** — Dockerfile.arm64/windows, PortMaster, пресеты
7. [x] **Полировка** — Тесты, линтер, README, финальная упаковка

---

## Roadmap v0.2+ (после UI-рефакторинга)

Приоритет P1 — низкий риск, высокая ценность, используют уже существующую
инфраструктуру (Settings page, Library, app.go event loop).

### [x] P1 — Режимы воспроизведения (shuffle/repeat)
- [x] `Library`: слой индирекции порядка (shuffle bag) поверх текущего
  последовательного `AlbumNext`/`TrackNext`, чтобы не переписывать сканер.
- [x] Repeat: Off / RepeatOne / RepeatAll — простой enum, проверяется в месте,
  где сейчас происходит "трек закончился → следующий".
- [x] Shuffle: Off / Shuffle Album / Shuffle Local / Shuffle All, персистится в config.json (`PlaybackSettings` рядом
  с `GraphicsSettings`).
- [x] UI: новая строка(и) в Settings page через `SettingRow`/`BuildSettingsRows` механизм.

### [x] P1 — Автопереключение пресетов по таймеру
- [x] В app.go используется `randPreset()` + `pending`-переход (плавная смена).
- [x] Добавлен тикер в главном цикле, интервал настраивается в Settings (Off/15s/30s/60s/2m).
- [ ] Стретч: переключение по "hard cut" пресетов с `!`-префиксом синхронно с
  битом (projectM уже поддерживает эту логику для собственных транзишенов).

### [ ] P2 — Захват звука с микрофона
- [ ] Обнаружение устройства при старте: `SDL_GetNumAudioDevices(1)`. Если 0 —
  пункт настроек не показываем вообще.
- [ ] Захват через SDL2 audio capture callback → пуш сэмплов в SoLoud
  buffer/queue источник, проигрываемый через мастер-шину (как обычный трек).
  Это позволяет переиспользовать существующий `GetWave()`/`CalcFFT()` →
  projectM конвейер без изменений — визуализация просто "видит" то, что
  играет сейчас, будь то файл или микрофон.
- [ ] UI: переключатель "Источник звука: Плеер / Микрофон" в Settings, виден
  только если устройство найдено.
- [ ] Риски: буферизация/задержка захвата, поведение при отключении устройства
  на лету (переключаться обратно на плеер).

### [ ] P3 — Прочие идеи
- [ ] Избранное/чёрный список пресетов (пропускать в случайной ротации),
  персистится в config.json.
- [x] Восстановление последней позиции воспроизведения при перезапуске
  (трек/секунда). Удалённые каталоги восстанавливаются только после успешной
  проверки сети; offline-запуск их игнорирует.
- [x] Beat sensitivity — projectM API, настраивается через Settings.
- [ ] Индикатор громкости на экране при регулировке.

---

## Ссылки

- Zimlite: `~/workspace/zimlite`
- SoLoud: https://solhsa.com/soloud/ (vendored src + include)
- projectM: https://github.com/projectM-visualizer/projectm (vendored)
- libopenmpt: https://lib.openmpt.org/ (системная зависимость)
- PortMaster: https://portmaster.games/
