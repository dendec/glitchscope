# Favorites — implementation notes

> Статус: реализовано. Этот файл сохраняет решения и UX-контракт; текущее
> поведение и владение описаны в [архитектурном документе](../ARCHITECTURE.md) и
> `internal/player/favorites.go`.

В source root при наличии избранных треков отображается виртуальная папка `favorites/`. В ней находятся
только непустые плейлисты Star, Heart и Note с соответствующими bitmap-иконками.
Один трек может входить
только в один плейлист.
Любая из трёх отметок также закрепляет скачанный remote-трек: автоматическая
очистка Downloads по сроку или лимиту его не удаляет.
Явное Delete download также блокируется, пока отметка Favorite не снята.

При запуске трека из favorites текущей playback queue становится снимок всего
плейлиста в порядке добавления. Последующие изменения favorites не меняют уже
запущенную queue.

Назначение выполняется на выбранном playable track:

- `F` или короткое нажатие Nintendo X циклически меняет membership:
    `None -> Star -> Heart -> Note -> None`;
- `Delete` или удержание Nintendo X 500 ms удаляет трек из любого плейлиста;
- действие применяется к строке под cursor, без fallback на playing track;
- folders, source root и остальные UI pages эти действия игнорируют.

## 1. Владение и хранение

`internal/player` владеет моделью favorites, нормализацией track path и JSON
storage. `internal/app` связывает её с input, playback queue и UI.
`internal/ui` только отображает read-only view и владеет навигацией.

Файл `favorites.json` хранится рядом с `settings.json`. Путь возвращает
`config.FavoritesPath()`.

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

- missing file означает пустые writable favorites;
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
read-only view. Favorite actions отключены, исходный файл не перезаписывается.
Сохранения на `App.Close` нет: каждая мутация уже записана атомарно.

Новый владелец и эти инварианты добавляются в `docs/ARCHITECTURE.md`.

## 2. Модель — `internal/player/favorites.go`

```go
type PlaylistID string

const (
    PlaylistStar  PlaylistID = "star"
    PlaylistHeart PlaylistID = "heart"
    PlaylistNote  PlaylistID = "note"
)

type PlaylistSpec struct {
    ID     PlaylistID
    Label  string
}

func PlaylistSpecs() []PlaylistSpec

type Favorites struct {
    playlists map[PlaylistID][]string
    lookup    map[string]PlaylistID
    path      string
    writable  bool
}

func LoadFavorites(path string) (*Favorites, error)
func NewReadOnlyFavorites() *Favorites

func (f *Favorites) GetPlaylist(trackPath string) PlaylistID
func (f *Favorites) Tracks(id PlaylistID) []string
func (f *Favorites) Count(id PlaylistID) int
func (f *Favorites) TotalCount() int

func (f *Favorites) Cycle(trackPath string) (PlaylistID, error)
func (f *Favorites) Remove(trackPath string) error
```

`Tracks` и `PlaylistSpecs` возвращают копии. `Cycle` выполняет:

```text
None -> Star -> Heart -> Note -> None
```

Path удаляется из старого массива и добавляется в конец нового. Порядок
остальных элементов не меняется.

## 3. Input

Новые действия: `ActionFavorite` и `ActionFavoriteRemove`.

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
func (in *Input) PollFavoriteHold(favouriteMode bool, now time.Time) Action
```

В favourite mode button-down начинает hold. Release до 500 ms возвращает cycle;
достижение порога возвращает remove один раз, а release после него ничего не
делает. Poll вызывается один раз за frame перед SDL events. Hold сбрасывается при
выходе из favourite mode, disconnect и `Input.Close`. Тесты используют
переданный `now`, без sleep.

## 4. App и async playback

После settings app загружает favorites и передаёт overlay read-only view. При
ошибке используется `NewReadOnlyFavorites()` и показывается уведомление.

Favorite actions применяются только к `overlay.SelectedTrack()`. После успешной
мутации app вызывает `overlay.RefreshFavorites()`. Ошибка записи показывается
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

Добавляются `sourceFavorites`, `ctxFavorites`, `entryFavoriteFolder` и
`entryFavoriteTrack`. Это virtual entries: они не проходят через `stat`,
filesystem delete или rescan. Playlist ID передаётся через `navEntry.format`
как строка ("star", "heart", "note").

Навигация использует существующие `navStack`, `pushLevel`, `popLevel`, cursor,
scroll, focus, Back и Enter:

```text
source root -> Favorites/ -> playlist -> tracks
```

- `favorites/` отображается при наличии избранных треков;
- внутри видны только непустые playlists в порядке `PlaylistSpecs()`;
- Enter на playlist открывает tracks в порядке добавления;
- Enter на track запускает async playback с pending queue;
- Back возвращает в `favorites/`, затем в source root;
- display name — существующий `player.TrackTitle(path)`.

Overlay хранит read-only interface:

```go
type FavoritesView interface {
    GetPlaylist(path string) player.PlaylistID
    Tracks(id player.PlaylistID) []string
    Count(id player.PlaylistID) int
}

func (o *Overlay) SetFavorites(view FavoritesView)
func (o *Overlay) RefreshFavorites()
func (o *Overlay) SelectedTrack() (SelectedTrack, bool)
```

Refresh перестраивает source root и открытый favourite playlist. Выбранный path
сохраняется, если он остался; иначе cursor clamp-ится. После удаления последнего
track overlay возвращается в `favorites/`.

В списках треков справа отображается bitmap-иконка Star, Heart или Note,
включая списки внутри Favorites. Ячейка равна высоте строки; имена и marquee
не заходят в колонку отметок. Теперь символами основного шрифта эти отметки
не рисуются; модель предоставляет playlist ID, UI выбирает соответствующий bitmap.
F-действие игнорируется при просмотре внутри favorites playlist.
Hint `Fav` показывается для playable track без отметки, `Remove` — с отметкой.

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

- пустые favorites и playlists скрыты;
- Back, Enter, cursor и scroll используют общий nav stack;
- selection возвращает полный playlist и index;
- refresh сохраняет выбранный path и закрывает пустой playlist;
- symbols и hints зависят от membership.

## 7. Порядок реализации

1. **Core:** model, strict load, transactional storage, path helper, tests,
   `favoritesPath()` и architecture doc.
2. **Input:** actions, context-aware short/long press и tests.
3. **App/playback:** load policy, handlers, request-ID pending queue и tests.
4. **UI:** virtual navigation, refresh, indicators, hints, font и tests.

После каждой фазы: `make test` и `make lint`. После изменения font и packaging:
`make dist`. Каждая фаза может быть закоммичена отдельно.
