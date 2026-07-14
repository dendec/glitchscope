# MDPP — UI Tasks

## ✅ 1 — Unicode ranges in font_ranges.json
- [x] Добавить Geometric Shapes, Misc Technical, Box Drawing, Block Elements.

## ✅ 2 — SoLoud C-обёртки → Go
- [x] bridge.cpp: `Soloud_getStreamTime`, `Wav_getLength`, `Soloud_getSamplerate`, `Soloud_getInfo`.
- [x] soloud.go: Go wrappers для них.

## ✅ 3 — Metadata cache (new file)
- [x] `internal/player/meta.go` — `TrackMeta`, `albumMeta`, `readMetaCache()`, `writeMetaCache()`.

## ✅ 4 — Player: позиция, длительность, метаданные
- [x] `Position()`, `Duration()`, `SampleRate()`, `Channels()`, `BPM()`, `IsPaused()`, `TrackPath()`.
- [x] BPM/channels сохраняются при трекерных треках через openmpt.
- [x] `openmpt.DecodeToF32` возвращает bpm и channels.
- [x] `openmpt.GetTrackerMeta` — лёгкая функция без декодинга аудио.

## ✅ 5 — Library: TrackInfo, ленивое сканирование
- [x] `TrackInfo` struct.
- [x] `GetAlbumTracks(idx int) []TrackInfo` — читает кеш, вычисляет недостающие.
- [x] `SelectAlbum(idx)`, `SelectTrack(idx)`, `CurrentAlbumIndex()`, `CurrentTrackIndex()`.

## ✅ 6 — Input: новые экшны
- [x] `ActionSelect`, `ActionBack`, `ActionToggleUI`, `ActionFocusLeft/Right`, `ActionCursorUp/Down`.
- [x] Gamepad DPAD → cursor/focus, Back → ToggleUI.
- [x] Keyboard Tab → ToggleUI, Enter → Select, Backspace → Back.

## ✅ 7 — Overlay: большая переработка
- [x] Новый `Overlay` struct: `uiVisible`, панели, dirty-флаги, кешированные текстуры.
- [x] `drawFilledRect` — новый шейдер для прямоугольников.
- [x] `renderUI`: альбомы (слева) + треки (справа) + нижняя панель (прогресс, время, тех-инфо).
- [x] Старая нотификация сохранена (ShowTrack/Inject/hide).
- [x] `ToggleUI()`, `CursorUp/Down()`, `FocusLeft/Right()`, `Select()`, `Back()`.
- [x] Геттеры `AlbumCursor()`, `TrackCursor()`, `FocusPanel()`.

## ✅ 8 — main.go: два режима диспетчеризации + FPS
- [x] `handleUIAction` / `handleNormalAction` — раздельная диспетчеризация.
- [x] FPS-счётчик: скользящее среднее по 60 кадрам.
- [x] Данные плеера пушатся в overlay каждый кадр.
- [x] Track list обновляется только при смене альбома.
