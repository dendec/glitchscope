# План: текстовый UI для MDPP

## Архитектура — 2 режима

```
Overlay скрыт (Select toggle off)
  → DPAD: треки/альбомы, как сейчас (NextTrack/PrevTrack/NextAlbum/PrevAlbum)
  → Оверлей показывает только трек-нотификацию (fade in/out + injection в feedback)
  → Кнопки A/B: PlayPause / ToggleOverlay (как сейчас)

Overlay видим (Select toggle on)
  → DPAD: навигация по UI-панелям
  → A (Select): выбрать элемент (альбом → играть, трек → играть)
  → B (Back): вернуться на уровень назад (треки → альбомы, альбомы → скрыть UI)
  → Визуализация не прерывается (всё полупрозрачное)
  → Кнопки PlayPause / ToggleOverlay работают всегда, независимо от режима UI
```

### Управление кнопками (в UI режиме)

| Кнопка          | Экшн              | Поведение                                  |
|-----------------|-------------------|--------------------------------------------|
| Select (геймпад Back / Tab) | ToggleUI     | Показать/скрыть UI                         |
| A (геймпад B)   | PlayPause         | Воспроизведение/Пауза                      |
| B (геймпад A)   | Old ToggleOverlay | Показать/скрыть нотификацию (старое)       |
| D-Pad Up/Down   | CursorUp/Down     | Перемещение внутри активной панели         |
| D-Pad Left/Right| FocusLeft/Right   | Переключение фокуса: альбомы ↔ треки       |
| A (Enter)       | Select            | Выбрать элемент (альбом/трек)              |
| B (Backspace)   | Back              | Уйти на уровень назад                      |

## Макет (ASCII)

```
┌ FPS:60 ─────────────────────────────────────┐
│                                               │
│  ┌─ Albums ─────────────┐ ┌─ Tracks ────────┐│
│  │ Album One             │ │ 01 Track    3:45 ││
│  │▸Album Two             │ │ 02 Track    4:20 ││
│  │ Album Three           │ │▶03 Current  5:10 ││ ← зелёный
│  │ ...                   │ │ 04 Track    2:30 ││
│  └──────────────────────┘ └─────────────────┘│
│                                               │
│ ████████████████░░░░░░░░░ 01:23 / 04:56       │ ← прогресс-бар + время
│ ▶ Play    ⏭ Next    ⏮ Prev    44.1kHz stereo │
│                       Tracker: 6ch, 125 BPM   │ ← тех-инфо
└────────────────────────────────────────────────┘
```

## Поведение панелей

- Левая панель (`Albums`): список альбомов. `Up/Down` двигает курсор.
- Правая панель (`Tracks`): треки альбома, на который указывает курсор левой панели.
  Обновляется мгновенно при смене курсора в альбомах.
- Фокус: левая панель (по умолчанию). `Left/Right` переключает фокус между панелями.
- Выбор альбома (`A` при фокусе на альбомах): воспроизведение с первого трека выбранного альбома.
- Выбор трека (`A` при фокусе на треках): воспроизведение выбранного трека.
- Текущий играющий трек подсвечен **зелёным**. Если играющий трек не из текущего альбома —
  в правой панели нет зелёного выделения, пока курсор не перейдет на играющий альбом.

## Unicode-символы

Используются для кнопок, прогресс-бара, разделителей. Добавить в `font_ranges.json`:

| Блок                | Диапазон           | Символы                            |
|---------------------|--------------------|------------------------------------|
| Geometric Shapes    | U+25A0–25FF        | ▶ ▸ █ ░ ★                         |
| Misc Technical      | U+2300–23FF        | ⏸ ⏹ ⏭ ⏮                         |
| Box Drawing         | U+2500–257F        │ │ ─ ┌ ┐ └ ┘ ◀ ▶              |
| Block Elements      | U+2580–259F        | █ ░ ▒ ▓                           |

## Кеш метаданных треков (`.mdpp_meta.json`)

Файл в каждой папке альбома. Содержит длительность, BPM, каналы для каждого трека.

```json
{"v":1,"t":{"song.mod":{"d":125.5,"b":140,"c":6}}}
```

- `v` — версия формата (для обратной совместимости)
- `d` — duration (сек, float64)
- `b` — BPM (только трекер, float64)
- `c` — channels (только трекер, int)

При старте: читаем файл. Для треков, которых нет в кеше — вычисляем через libopenmpt (трекер)
или SoLoud `Wav_load` → `getLength` (аудио) и дописываем в файл. Первый раз медленно, потом мгновенно.

## Порядок реализации

### Шаг 1: Unicode-диапазоны

Добавить блоки в `font_ranges.json`. `pyftsubset` сам отфильтрует нужные глифы.

### Шаг 2: SoLoud C-обёртки → Go

Обернуть в `bridge.cpp` + `soloud.go`:
- `Soloud_getStreamTime(voice) → float64` — позиция
- `Wav_getLength(wav) → float64` — длительность
- `Soloud_getSamplerate(voice) → float32` — частота
- `Soloud_getInfo(voice, "channels") → float32` — каналы (через `getInfo`)

### Шаг 3: Metadata cache

- `internal/player/meta.go` — `TrackMeta` struct, `readMetaCache()`, `writeMetaCache()`

### Шаг 4: Player — позиция, длительность, метаданные

- `Position() float64` — `Soloud_getStreamTime()`
- `Duration() float64` — `Wav_getLength()`
- `SampleRate() float32`
- `Channels() int` — через `getInfo(key)`
- `BPM() float64` — хранится из `playTracker()`
- `IsPaused() bool`
- При трекерном треке: сохранять `bpm` и `channels` из `openmpt_module_get_metadata()`

### Шаг 5: Library — TrackInfo, ленивое сканирование

- `TrackInfo` struct: `{Path string, Duration float64, BPM float64, Channels int}`
- `GetAlbumTracks(idx int) []TrackInfo` — читает кеш, вычисляет недостающие
- В `NewLibrary()` проверяет наличие `.mdpp_meta.json`, но не сканирует

### Шаг 6: Input — новые экшны

```go
ActionSelect    // A/Enter
ActionBack      // B/Backspace  
ActionToggleUI  // Select/Tab
ActionFocusLeft // Left (когда UI видим)
ActionFocusRight // Right (когда UI видим)
ActionCursorUp   // Up (когда UI видим)
ActionCursorDown // Down (когда UI видим)
```

Input создаёт эти экшны. `main.go` решает, как их обрабатывать — как навигацию по UI или
как смену треков/альбомов — в зависимости от `overlay.UIVisible()`.

### Шаг 7: Overlay — большая переработка

**Новый `Overlay` struct:**

```go
type Overlay struct {
    face  font.Face       // кешируется в New()
    programText  uint32   // шейдер для текста (существующий)
    programRect  uint32   // шейдер для залитых прямоугольников (новый)

    // Режим
    uiVisible   bool      // Select тогглит
    notifVisible bool     // трек-нотификация
    notifHidden  bool     // B подавляет нотификацию (существующее)

    // Данные для UI (обновляются из плеера каждый кадр)
    albums       []string
    albumCursor  int
    trackInfos   []player.TrackInfo
    trackCursor  int
    position     float64
    duration     float64
    sampleRate   float32
    channels     int
    bpm          float64
    paused       bool
    fps          float64
    playingAlbum string      // какой альбом сейчас играет
    playingTrack string      // какой трек сейчас играет (для зелёного)
    focusPanel   int         // 0 = albums, 1 = tracks

    // Кешированные текстуры панелей
    albumsTex  uint32; albumsTexW, albumsTexH int
    tracksTex  uint32; tracksTexW, tracksTexH int
    bottomTex  uint32; bottomTexW, bottomTexH int
    fpsTex     uint32; fpsTexW, fpsTexH int
    notifTex   uint32; notifTexW, notifTexH int  // (существующее)
    
    // Dirty flags (что надо перерендерить)
    albumsDirty bool
    tracksDirty bool
    bottomDirty bool
    fpsDirty    bool
}
```

**Примитивы:**

1. `drawRect(x, y, w, h, r, g, b, a)` — новый шейдер, только `uColor` uniform, без текстур
2. `drawTextCached(x, y, texID, texW, texH)` — существующий текстурный шейдер
3. `renderTextToTex(text string, fontSize int) → (texID, w, h)` — функция, рендерит строку в RGBA, загружает в GL

**Рендеринг кадра:**

```go
func (o *Overlay) Draw(width, height int) {
    if o.uiVisible {
        o.renderUI(width, height)
    } else if o.notifVisible && !o.notifHidden && !o.injected {
        o.renderNotification(width, height)
    }
}

func (o *Overlay) renderUI(w, h int) {
    // 1. Залить фон панелей полупрозрачным чёрным
    drawRect(0, 0, w, h, 0, 0, 0, 0.3)  // весь экран слегка затемнён

    // 2. FPS (левый верхний угол, кеширован)
    if o.fpsDirty { re-render fps text }
    drawTextCached(5, 5, o.fpsTex, o.fpsTexW, o.fpsTexH)

    // 3. Левая панель альбомов
    //    Прямоугольник + текст каждого альбома
    if o.albumsDirty { re-render album list to tex }
    drawTextCached(panelX, panelY, o.albumsTex, ...)

    // 4. Правая панель треков
    if o.tracksDirty { re-render track list to tex }
    drawTextCached(tracksPanelX, tracksPanelY, o.tracksTex, ...)
    
    // 5. Нижняя панель: прогресс-бар, кнопки, время, тех-инфо
    if o.bottomDirty { re-render bottom bar to tex }
    drawTextCached(0, h-bottomH, o.bottomTex, ...)
}
```

### Шаг 8: main.go — два режима диспетчеризации

```go
func handleAction(act input.Action, ...) {
    if overlay.UIVisible() {
        switch act {
        case ActionQuit:         os.Exit
        case ActionPlayPause:    pl.TogglePause()
        case ActionToggleUI:     overlay.ToggleUI()
        case ActionCursorUp:     overlay.CursorUp()
        case ActionCursorDown:   overlay.CursorDown()
        case ActionFocusLeft:    overlay.FocusLeft()
        case ActionFocusRight:   overlay.FocusRight()
        case ActionSelect:       overlay.Select() // играет выбранный трек/альбом
        case ActionBack:         overlay.Back()
        }
    } else {
        // Существующая логика (NextTrack, PrevAlbum, etc.)
    }
}
```

FPS-счётчик: замерять `time.Since(lastFrame)` в начале тика, скользящее среднее по 60 кадрам,
передавать в overlay.SetFPS().

## Изменяемые файлы

| Файл                      | Действие              |
|---------------------------|-----------------------|
| `font_ranges.json`        | add Unicode blocks    |
| `internal/soloud/bridge.cpp` | add 4 C wrappers   |
| `internal/soloud/soloud.go`  | add 4 Go wrappers  |
| `internal/player/meta.go` | NEW — metadata cache  |
| `internal/player/player.go`  | add accessors + meta storage |
| `internal/player/library.go` | add TrackInfo, lazy scan |
| `internal/input/input.go` | add ActionSelect/Back/ToggleUI/Focus/Cursor |
| `internal/ui/overlay.go`  | major rewrite         |
| `cmd/mdpp/main.go`        | dual-mode dispatch, FPS |

## Что НЕ делаем (сейчас)

- Курсор не рисуем (но дизайн его учитывает — будет курсор позже)
- Скроллинг длинных списков (первая версия с небольшим числом альбомов)
- Seek (перемотка) — только PlayPause
- Volume control
- Эквалайзер / визуализация FFT
- Экран настроек
- Загрузка обложек
- Поддержка плейлистов
