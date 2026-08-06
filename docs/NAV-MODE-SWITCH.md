# Навигация NC ↔ Provider — архитектурный дефект

Статус: ИСПРАВЛЕНО (единый навигационный стек). Внесено: overlay.go,
overlay_nav.go, overlay_input.go, render_library.go, overlay_test.go.
Проверено: `make test`, `make lint`, `make dist` — зелёные.

## Симптомы (одно поведение, три проявления)

Вход в удалённый каталог или выход из него не выполняет фактический
переход: режим панелей переключается, а список корня подменяется.
Файлы NC-корня (реальная ФС) исчезают, правая панель меняет семантику.

1. Вход в Modland из NC-корня — БЫЛ сломан, сейчас работает.
2. Вход в ModArchive из NC-корня — СЛОМАН.
3. Выход из обоих каталогов — СЛОМАН.

## Текущая архитектура

Две независимые навигационные модели на общем состоянии:

| | Provider | NC |
|---|---|---|
| Стек | `navStack []navLevel` (`overlay.go:89`) | `ncStack []navLevel` (`overlay.go:137`) |
| Корень | `rootEntries` = `buildRootEntries()` из альбомов библиотеки | `buildNCDirectoryEntries(ncPath)` из реальной ФС |
| Позиция | `albumCursor/albumsScroll` | `albumCursor/albumsScroll` (общие) |
| Правая панель | превью (previewEntries) | NC-инфо (ncInfoFile/Dir) |

Переключатель: `libMode` (`libModeProvider`/`libModeNC`, `overlay.go:55-56`).
Рендер зависит от него: `render_library.go:106,154` (левый список, правая панель).

## Причины

### 1. Вход — две неатомарные мутации

Вход в каталог из NC = ДВА связанных шага (`overlay_input.go:625-633`):

```go
o.ncSwitchToProvider()          // шаг 1: смена режима + пересборка корня
return o.enterModland()         // шаг 2: фактический вход (pushLevel)
```

`ncSwitchToProvider()` (`overlay_nav.go:870-883`) уже мутирует всё видимое:
`libMode=Provider`, `navStack=nil`, `rootEntries=buildRootEntries()`,
`albumEntries=rootEntries`, курсор=0, `syncPanels()`.

`enterModland()` — единственный шаг, который даёт видимый переход
(`pushLevel`). Если он не сработал — пользователь уже сидит на
provider-корне: NC-список ФС заменён библиотечным листингом, правая
панель переключилась на превью. Ни один из шагов не атомарен; инвариант
«переключение режима ⇒ немедленный вход» нигде не гарантирован.

### 2. Почему Modland работает, а ModArchive — нет

`enterModland()` → `pushLevel(buildFormatEntries())`. Список форматов
строится из in-memory `allAlbums` (`overlay_nav.go:351-370`) — всегда
непустой, если каталог загружен. Push безусловный ⇒ вход всегда
происходит. (Починено в `b5119a0`; до этого в `7a7031c` `enter*` не
вызывался вовсе — тот же класс бага.)

`enterModArchive()` (`overlay_nav.go:512-518`):

```go
entries := o.buildModArchiveEntries(modarchive.BaseURL)
if len(entries) > 0 {
    o.pushLevel(entries)
}
return false
```

`buildModArchiveEntries()` при первом входе возвращает `nil` — кэша нет,
стартует асинхронный fetch (`overlay_nav.go:394-400`). Guard не пушит.
`applyModArchiveResults()` по приходу данных только обновляет превью
(`overlay_nav.go:431-436`) — уровень не пушит. Итог: нужен второй Enter.
При этом существует синхронный `modarchive.FetchDirectoryCached`
(`modarchive/modarchive.go:279`) — in-memory/дисковый кэш без сети.
Async-обвязка в overlay (`modArchivePending`, `modArchiveResults`,
`requestModArchiveEntries`, `applyModArchiveResults`) — дублирующий
мёртвый код: данные всегда уже закэшированы локально.

### 3. Выход не знает, откуда пришли

`Back()` (`overlay_input.go:521-527`):

```go
if o.popLevel() { return }              // из drill-down — на уровень выше
if o.libMode == libModeProvider {       // на корне provider
    o.ncSwitchToNC()
    return
}
```

`popLevel()` возвращает на `rootEntries` — provider-корень (библиотечный
листинг). Но если каталог открывали из NC-корня, ожидание — один Back до
NC-вида. Факт: первый Back показывает чужой список (NC-файлы «исчезли»),
второй — `ncSwitchToNC()` — возвращает NC. Происхождение (NC,
`ncPath`, курсор) хранится не в стеке, а в одноуровневых полях
`ncRootCursor/ncRootScroll` + `ncPath` (`overlay_nav.go:871-872`) — память
только об одном уровне перехода, не о всей истории.

### 4. Общий корень

1. Смена режима (libMode + пересборка корня) — побочный эффект, отделённый
   от самого перехода (push). Возможна смена режима БЕЗ перехода.
2. Два параллельных стека + переключатель. Инвариант «какой стек активен»
   не хранится в данных стека — восстанавливается специальными функциями
   (`ncSwitchToNC`, `ncSwitchToProvider`), а не общим механизмом.
3. Возврат (`popLevel`) строится вокруг provider-модели и не знает о
   контексте, из которого пришли.

## Решения по продукту (зафиксировано)

1. **Back изнутри каталога — один, сразу в NC-корень.** Никакого
   промежуточного «корня каталога» (библиотечного листинга) на пути назад.
2. **Каталоги открываются ТОЛЬКО из NC-корня.** Строки Modland/ModArchive
   убираются из `buildRootEntries` (provider-корень каталоги не предлагает).
3. **Fetch при первом входе не существует — всё локально закэшировано.**
   Async-обвязка overlay удаляется; списки строятся синхронно из
   `FetchDirectoryCached`.
4. **Красивая архитектура без костылей** — единый стек, не патчи.

## Исправление: единый навигационный стек

### Модель

Убрать: `libMode`, `ncStack`, `ncPath`, `ncRootCursor`, `ncRootScroll`,
`ncSwitchToProvider`, `ncSwitchToNC`, `ncSync` (как переключатель),
async-обвязку modarchive.

Один стек — единственный источник правды. Каждый уровень несёт свой
контекст:

```go
type navCtx int

const (
    ctxNC      navCtx = iota // файловая система: dirPath
    ctxLibrary               // корень библиотеки (локальные альбомы)
    ctxCatalog               // каталог: форматы / альбомы / modarchive-папки
)

type navLevel struct {
    ctx     navCtx
    entries []navEntry
    cursor  int
    scroll  int
    dirPath string // ctxNC: каталог ФС
}
```

Дно стека — корневой уровень (`ctxNC` baseDir или `ctxLibrary`).
Видимый уровень = верх стека. `albumEntries/albums/albumCursor/albumsScroll`
становятся производными от верха (временные, при рендере) либо живут как
сейчас, но синхронизируются через единые `push`/`pop`/`setRoot`.

### Операции

- `push(level)` — вход в любую сущность: NC-папка, форматы каталога,
  альбомы формата, modarchive-папка, локальная папка. Одна мутация.
- `pop()` — Back: всегда на предыдущий уровень. Из форматов → NC-корень
  за один Back автоматически (форматы лежат прямо на NC-корне).
- `setRoot(ctx)` — замена стека одним корневым уровнем: Library↔NC
  (плейбэк-фокус, закрытие UI).

Выбор строки = `push(levelFromEntry(e))`; возврат = `pop()`. Одна
диспетчеризация вместо раздутых `ncSelect`/`Select`-веток.

### Конкретные шаги

1. `navLevel` получает `ctx` (+`dirPath`); `ncStack` удаляется, `ncPath`
   переносится в уровень `ctxNC`.
2. Удалить `libMode` и все проверки `o.libMode == libModeNC` в
   `overlay_input.go` (177, 252, 309, 343, 370, 420, 505, 515, 526),
   `overlay.go` (360, 488, 508), `render_library.go` (61, 69, 106, 154).
   Вместо: `o.top().ctx == ctxNC` / по `kind` текущей записи
   (`IsNCDirectory/IsNCFile` → NC-инфо, остальные → preview).
3. `enterModland`/`enterModArchive` → просто `push`. Вызовы
   `ncSwitchToProvider()` из `ncSelect` удаляются (`overlay_input.go:625-633`).
4. `buildModArchiveEntries` — синхронно через `FetchDirectoryCached`,
   без guard на асинхронность; пустой каталог = пустой уровень (корректный
   вход, не пропуск). Удалить `modArchivePending`, `modArchiveResults`,
   `requestModArchiveEntries`, `applyModArchiveResults`, обработку в
   run_loop (вызов `applyModArchiveResults`).
5. `buildRootEntries` — без строк Modland/ModArchive (`overlay_nav.go:310-315`);
   строки остаются только в NC-листинге baseDir (`overlay_nav.go:776-780`).
6. `popLevel`/`ncBack`/`Back` схлопываются в один `pop()` + «на дне NC →
   закрыть UI, на дне Library → перейти в NC root».
7. Deep-link (`FocusPlayingTrack`): строит стек уровнями напрямую:
   `[root, formats, albums]`; убрать `rootCursor = rootModlandIndex()`,
   `rootModArchiveIndex`, `rootLocalDirIndex`, `rootCursor/rootScroll`
   (`overlay_nav.go:183-243`).
8. `refreshNCPreview`/`refreshPreview` — диспетчеризация по `kind` текущей
   записи (уже частично так), убрать ветку по `libMode` в `syncPanels`
   (`overlay_nav.go:553`).

### Открытые вопросы

- Судьба `ctxLibrary` (альбомная вью): **решено — сохранить** как второй
  корень (deep-link, стартовый вид). NC-позиция при remote deep-link
  сбрасывается на baseDir (стек заменяется) — осознанный трейд-офф
  единого стека, не возвращать сохранённое состояние.

## Итог (реализовано)

- `libMode`, `ncStack`, `ncPath`, `ncRootCursor/Scroll`,
  `ncSwitchToProvider/NC`, `ncSync` — удалены. `navCtx` на уровне стека.
- **Единый стек буквально**: корень всегда лежит в стеке как нижний
  элемент — `navStack[0]` c `ctx: ctxNC` (baseDir) или `ctx: ctxLibrary`.
  Поля `rootEntries/rootCursor/rootScroll` удалены, курсор/скролл корня
  хранятся в `navStack[0].cursor/scroll`. Пустой стек невозможен
  (`New()` инициализирует корень библиотеки); `isNC()` читает только верх
  стека, спецкейсов `len(navStack)==0` нет.
- Вход в каталог/папку = один `pushLevel(ctx, entries)`; Back = `popLevel`
  (корень не поппится); Back на NC-корне закрывает UI, на Library-корне —
  переключает на NC.
- Async-обвязка modarchive удалена; списки синхронно из
  `FetchDirectoryCached` + оверлейный кэш `modArchiveItems`.
- Строки Modland/ModArchive — только в NC-листинге baseDir.
- Одна диспетчеризация `Select` по `kind` записи; `ncSelect` удалён.
- Тесты: синхронный кэш modarchive, round-trip NC↔каталог, Back на корнях.
