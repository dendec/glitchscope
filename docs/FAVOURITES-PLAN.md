# Favourites — план реализации

В source root всегда отображается виртуальная папка `Favourites/`. В ней находятся
только непустые плейлисты `★ Star`, `♥ Heart` и `♪ Note`. Один трек может входить
только в один плейлист.

При запуске трека из favourites текущей playback queue становится снимок всего
плейлиста в порядке добавления. Последующие изменения favourites не меняют уже
запущенную queue.

Назначение выполняется на выбранном playable track:

- `F` или короткое нажатие Nintendo X циклически меняет membership:
    `None -> Star -> Heart -> Note -> None`;
- `Delete` или удержание Nintendo X 500 ms удаляет трек из любого плейлиста;
- действие применяется к строке под cursor, без fallback на playing track;
- folders, source root и остальные UI pages эти действия игнорируют.

## 1. Владение и хранение

`internal/player` владеет моделью favourites, нормализацией track path и JSON
storage. `internal/app` связывает её с input, playback queue и UI.
`internal/ui` только отображает read-only view и владеет навигацией.

Файл `favourites.json` хранится рядом с `settings.json`. Путь возвращает
`config.FavouritesPath()`.

```json
{
  "version": 1,
  "playlists": {
    "star": ["/music/example.mod"],
    "heart": ["modarchive:http://example/track.mod"],
    "note": []
  }
}
```

Правила:

- missing file означает пустые writable favourites;
- malformed JSON, неизвестная версия или playlist ID, пустой path, неверный тип,
  trailing JSON и duplicate path возвращают ошибку;
- duplicate path внутри одного или разных плейлистов запрещён;
- отсутствующие local files сохраняются и дают ошибку только при запуске;
- порядок массивов не сортируется и является порядком воспроизведения.

Local path нормализуется при загрузке и перед мутацией через `filepath.Abs` и
`filepath.Clean`, без `EvalSymlinks`. Virtual path принимается только с известным
provider prefix и остаётся в canonical form provider-а.

Запись выполняется через temp file, close и rename. `Cycle` и `Remove`
транзакционны: изменённая копия записывается первой и только после успешного
rename заменяет in-memory state. Ошибка записи не меняет состояние.

Если существующий файл не загрузился, app показывает ошибку и использует пустой
read-only view. Favourite actions отключены, исходный файл не перезаписывается.
Сохранения на `App.Close` нет: каждая мутация уже записана атомарно.

Новый владелец и эти инварианты добавляются в `docs/ARCHITECTURE.md`.

## 2. Модель — `internal/player/favourites.go`

```go
type PlaylistID string

const (
    PlaylistStar  PlaylistID = "star"
    PlaylistHeart PlaylistID = "heart"
    PlaylistNote  PlaylistID = "note"
)

type PlaylistSpec struct {
    ID     PlaylistID
    Symbol rune
    Label  string
}

func PlaylistSpecs() []PlaylistSpec

type Favourites struct {
    playlists map[PlaylistID][]string
    lookup    map[string]PlaylistID
    path      string
    writable  bool
}

func LoadFavourites(path string) (*Favourites, error)
func NewReadOnlyFavourites() *Favourites

func (f *Favourites) GetPlaylist(trackPath string) PlaylistID
func (f *Favourites) Symbol(trackPath string) string
func (f *Favourites) Tracks(id PlaylistID) []string
func (f *Favourites) Count(id PlaylistID) int
func (f *Favourites) TotalCount() int

func (f *Favourites) Cycle(trackPath string) (PlaylistID, error)
func (f *Favourites) Remove(trackPath string) error
```

`Tracks` и `PlaylistSpecs` возвращают копии. `Cycle` выполняет:

```text
None -> Star -> Heart -> Note -> None
```

Path удаляется из старого массива и добавляется в конец нового. Порядок
остальных элементов не меняется.

## 3. Input

Новые действия: `ActionFavourite` и `ActionFavouriteRemove`.

| Устройство | Ввод | Результат |
|---|---|---|
| Keyboard | `F` | cycle |
| Keyboard | `Delete` | remove |
| Gamepad | Nintendo X short | cycle |
| Gamepad | Nintendo X held 500 ms | remove |

Nintendo X соответствует SDL `CONTROLLER_BUTTON_Y`.

При скрытом UI button-down сразу даёт прежний `ActionPlayPause`. Hold
распознаётся только когда открыта Library page и под cursor находится playable
track. На других страницах, folders и source root X ничего не делает.

`Input` получает контекст от app:

```go
func (in *Input) ProcessEvent(event sdl.Event, favouriteMode bool, now time.Time) Action
func (in *Input) PollFavouriteHold(favouriteMode bool, now time.Time) Action
```

В favourite mode button-down начинает hold. Release до 500 ms возвращает cycle;
достижение порога возвращает remove один раз, а release после него ничего не
делает. Poll вызывается один раз за frame перед SDL events. Hold сбрасывается при
выходе из favourite mode, disconnect и `Input.Close`. Тесты используют
переданный `now`, без sleep.

## 4. App и async playback

После settings app загружает favourites и передаёт overlay read-only view. При
ошибке используется `NewReadOnlyFavourites()` и показывается уведомление.

Favourite actions применяются только к `overlay.SelectedTrack()`. После успешной
мутации app вызывает `overlay.RefreshFavourites()`. Ошибка записи показывается
через notifier; UI и in-memory state остаются прежними.

Для playable entry overlay возвращает:

```go
type SelectedTrack struct {
    Path     string
    Playlist []string
    Index    int
    Label    string
}
```

Для favourite track `Playlist` — копия полного плейлиста. Для остальных tracks
он может быть nil, если существующая логика сама строит queue.

Queue нельзя устанавливать сразу после `PlayFileAsync`: это только начало
загрузки. `PlayFileAsync` возвращает `requestID`, а `CheckPending` — ID принятого
результата. App хранит pending playlist с тем же ID:

```go
type pendingPlaylist struct {
    requestID uint64
    tracks    []string
    index     int
    label     string
}
```

Queue заменяется только после успешного совпадающего результата. При ошибке,
отмене или новом playback request pending playlist очищается, а текущая queue не
заменяется. Тот же механизм исправляет существующий catalog playback, где queue
сейчас устанавливается до завершения async load.

## 5. UI и навигация

Добавляются `sourceFavourites`, `ctxFavourites`, `entryFavouriteFolder` и
`entryFavouriteTrack`. Это virtual entries: они не проходят через `stat`,
filesystem delete или rescan. Playlist ID передаётся через `navEntry.format`
как строка ("star", "heart", "note").

Навигация использует существующие `navStack`, `pushLevel`, `popLevel`, cursor,
scroll, focus, Back и Enter:

```text
source root -> Favourites/ -> playlist -> tracks
```

- `Favourites/` отображается всегда;
- внутри видны только непустые playlists в порядке `PlaylistSpecs()`;
- Enter на playlist открывает tracks в порядке добавления;
- Enter на track запускает async playback с pending queue;
- Back возвращает в `Favourites/`, затем в source root;
- display name — существующий `player.TrackTitle(path)`.

Overlay хранит read-only interface:

```go
type FavouritesView interface {
    GetPlaylist(path string) player.PlaylistID
    Symbol(path string) string
    Tracks(id player.PlaylistID) []string
    Count(id player.PlaylistID) int
}

func (o *Overlay) SetFavourites(view FavouritesView)
func (o *Overlay) RefreshFavourites()
func (o *Overlay) SelectedTrack() (SelectedTrack, bool)
```

Refresh перестраивает source root и открытый favourite playlist. Выбранный path
сохраняется, если он остался; иначе cursor clamp-ится. После удаления последнего
track overlay возвращается в `Favourites/`.

В track lists перед именем отображается `★`, `♥` или `♪` цветом
`palette().cursor`. Hint `Fav` показывается для любого playable track. Hint
`Remove` показывается только для track, который уже входит в favourites.

Glyphs `U+2605`, `U+2665` и `U+266A` добавляются в `font_ranges.json`; build
проверяет их наличие в итоговом OTF.

## 6. Проверки

### Player/storage

- missing file, round trip и полный cycle;
- строгая schema, trailing JSON и неизвестные IDs;
- duplicates внутри и между playlists;
- порядок и defensive copies;
- ошибка записи не меняет состояние;
- invalid load не позволяет перезаписать исходный файл.

### Input

- short/long press дают одно правильное действие;
- hidden UI сохраняет немедленный play/pause;
- смена mode и disconnect сбрасывают hold.

### App/playback

- actions работают только для playable track;
- queue коммитится после успешного совпадающего request ID;
- failed, cancelled и stale requests не заменяют queue;
- механизм работает для catalog и favourite tracks.

### UI

- `Favourites/` присутствует при пустом состоянии, пустые playlists скрыты;
- Back, Enter, cursor и scroll используют общий nav stack;
- selection возвращает полный playlist и index;
- refresh сохраняет выбранный path и закрывает пустой playlist;
- symbols и hints зависят от membership.

## 7. Порядок реализации

1. **Core:** model, strict load, transactional storage, path helper, tests,
   `FavouritesPath()` и architecture doc.
2. **Input:** actions, context-aware short/long press и tests.
3. **App/playback:** load policy, handlers, request-ID pending queue и tests.
4. **UI:** virtual navigation, refresh, indicators, hints, font и tests.

После каждой фазы: `make test` и `make lint`. После изменения font и packaging:
`make dist`. Каждая фаза может быть закоммичена отдельно.
