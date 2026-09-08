# Архитектура GlitchScope

Этот документ описывает фактические границы модулей. Детали поведения UI
зафиксированы в [UI-PLAN.md](UI-PLAN.md), сборка и проверки — в [README.md](../README.md)
и [Makefile](../Makefile).

## Владельцы истины

| Понятие | Единственный владелец | Остальные проекции |
|---|---|---|
| Настройки | `internal/config` | JSON storage, Settings UI |
| UI translations | `internal/i18n` | `internal/config` language choice, UI/help/notifications |
| Локальный индекс музыки | `internal/player` | NC navigation, playback selection, catalog albums |
| Навигация и focus | `internal/ui` | rendered lists and breadcrumbs |
| Playback queue | `internal/app` playback state | player commands, overlay snapshot |
| Удаление файлов | `internal/app/delete_service.go` | confirmation UI, rescan |
| Favorites | `internal/player` (`favorites.go`) | overlay navigation, input actions, JSON storage |
| Remote catalogs | `internal/modland`, `internal/modarchive` | provider navigation and downloads |
| Provider-neutral directory cache | `internal/catalog` | provider loaders, shuffle selection, UI listings |
| Downloaded track cache policy | `internal/player.TrackCache` | `internal/config`, downloader wiring, catalog UI |

**Каталоговые альбомы** (modland/modarchive) хранятся в `lib.Albums` через
`Library.AddCatalogAlbum`. Overlay создаёт альбомы on-the-fly во время навигации
и делегирует создание в Library через callback `SetAddCatalogAlbum`. Overlay
является только view — не хранит альбомы самостоятельно: каждый кадр он
снимает snapshot `lib.Albums` (`cachedAlbums`, обновляется в `Update()` и
после каждой мутации) и все потребители кадра читают один и тот же список.

**Виртуальные пути ↔ локальный кэш** (`.cache/modland/files`,
`.cache/modarchive/files`) резолвит `internal/player.Resolver`
(`resolver.go`) — единственный владелец правил cache-путей. `Library`
использует его как зависимость (`GetAlbumTracks`, флаг `TrackInfo.Cached`);
сам Library отвечает только за индекс альбомов. Извлечение метаданных из
локального файла — чистая функция `extractMetaFromFile` (`meta.go`), без
side effects, переиспользуется при on-demand заполнении и refresh комментария.

`internal/player.TrackCache` управляет только скачанными треками внутри этих
двух `files`-директорий. Индексы каталогов и bundled ModArchive GSA не входят в
лимит и никогда не удаляются cache policy. Время модификации файла является
восстанавливаемой отметкой последнего воспроизведения. Очистка сначала удаляет
файлы старше `settings.track_cache.retention`, затем применяет LRU до
`settings.track_cache.max_bytes`; активный трек защищён до остановки.
Первый startup-prune запускается в фоне после входа в render loop, поэтому
сканирование большого кэша не блокирует инициализацию приложения. UI применяет
готовую офлайн-проекцию на основном потоке после завершения задачи.

**ModArchive snapshot 1987-2007** представлен как дерево виртуальных папок:
буква → bucket ZIP → треки. Официальный addendum 2007 представлен соседним
источником с bucket ZIP верхнего уровня. `internal/modarchive` получает central
directory bucket ZIP хвостовым HTTP Range-запросом и кэширует индекс; сборка
поставляет все известные индексы основного snapshot в
`.cache/modarchive/1980-2007.gsa`, а addendum — в
`.cache/modarchive/2007-addendum.gsa`. GSA хранит каждый bucket независимо
сжатым и читает его лениво, поэтому навигация по обоим источникам не требует
сети и не загружает весь индекс в память. Выбранный entry скачивается отдельным
Range от его local header до следующего local header.
Полный внешний ZIP не скачивается даже как fallback: ответ без `206 Partial
Content` считается ошибкой. Прямые module entries распаковываются из `store`
или `deflate`; вложенные однотрековые ZIP после этого извлекаются локально.
Виртуальный track path хранит имя entry во fragment внешнего URL, а итоговый
cache path по-прежнему определяет только `internal/player.Resolver`.

Для будущего lazy shuffle `internal/catalog` содержит только provider-neutral
типы ключей, listing values, fingerprints и bounded directory cache. Провайдеры
владеют декодированием и persistent index records, `internal/app` — политикой
выбора и playback orchestration, а `internal/player.Library` — только текущим
playback/navigation context и materialized catalog albums, а не полным remote
shuffle pool. Подробный migration contract находится в
[shuffle-optimization.instructions.md](../.github/instructions/shuffle-optimization.instructions.md).

## Правила изменений

Изменение понятия считается завершённым только после обновления всех его
проекций: реализации, тестов, документации и build/package inputs. Новый
модуль не должен становиться обязательным владельцем нескольких независимых
решений.

`internal/filesystem` владеет общим обходом и статусами `OK`, `Partial` и
`Failed`. Пустой результат допустим только после успешного чтения. Playback
не заменяет существующий queue, если scan завершился `Partial` или `Failed`;
NC сохраняет статус listing для явного отображения неопределённости.

Настройки отсутствуют на первом запуске только в одном нормальном случае:
файл не существует. Повреждённый JSON и неизвестные значения возвращаются
как ошибки и не маскируются под корректное состояние.

Удаление сначала проверяет границу и symlink-политику, затем меняет файловую
систему. Playback и индекс обновляются только после успешного удаления;
ошибка rescan явно показывается как частично неизвестное состояние.

Основной цикл обрабатывает ввод и UI с частотой до 60 Гц, а главный projectM
планируется независимо с частотой выбранного performance mode. GL-операции остаются на закреплённом
main thread; между кадрами визуализации выводится последняя захваченная текстура.
Показатель `FPS` и adaptive resolution используют частоту завершённых кадров
визуализации, а не частоту итераций UI.

Во время плавной смены пресета adaptive resolution приостанавливается на
длительность projectM soft cut. При запуске перехода история adaptive policy
очищается; после перехода решение снова принимается только по новым завершённым
кадрам визуализации. Так временная стоимость смешивания двух пресетов не
понижает постоянное render resolution.

Если пробный upscale не выдерживает нижний FPS-порог и требует возврата, эта
верхняя ступень больше не пробуется для текущего пресета. Новый пресет или
переконфигурация adaptive resolution снимает ограничение.

## Проверки перед изменением

```text
make test
make lint
make dist
```

`make test` и `make lint` запускаются внутри builder image, чтобы результаты
не зависели от локальных cgo-зависимостей. `make dist` дополнительно проверяет
полный amd64 packaging path.

## Переключение и восстановление воспроизведения

Ручной Next учитывает shuffle и игнорирует Repeat One; без shuffle сохраняет
циклическую навигацию. Shuffle All без подтверждённого подключения выбирает
только локальный источник, включая fallback до готовности индексов. Наличие
кэшированных каталогов разрешает offline-навигацию, но не подтверждает сеть.
Подключение проверяется только при открытии Library или перед сетевой операцией.
Положительный и отрицательный результат хранятся одну минуту; одновременные
запросы объединяются в одну проверку с timeout 5 секунд. Корневые папки
Modland/ModArchive видимы всегда. Пока сеть неизвестна или недоступна, внутри
них отображаются только скачанные треки и родительские папки, ведущие к ним;
проигрывание скачанного трека не запускает сетевую проверку.

Ошибка загрузки пропускает трек, в том числе при Repeat One. Серия восстановления
хранит не более 10 неудачных путей и ограничивает поиск кандидата 10 попытками;
при исчерпании выбора показывает уведомление и прекращает автоматический перебор.
Успешный запуск или новый ручной выбор очищает серию ошибок.

Adaptive resolution применяет потолок режима при сбросе, resize и повышении:
Performance — native, Balanced — индекс 1 (0.75×), Eco — индекс 3 (0.5×).
Частота Performance — 30 FPS, Balanced и Eco — 24 FPS; cooldown измеряется
завершёнными кадрами визуализации, поэтому при просадках длится дольше.


## Handheld rendering and memory budgets

`internal/prof` reads available device memory, `internal/player` selects a
bounded tracker pre-render budget, and app wires the result at audio startup.
Settings owns only the `graphics.visualizer_off` rendering policy. The player
serializes native loads and owns the PCM budget calculation described in
SEEK-DESIGN.md.

Performance retains 60 Hz presentation. Balanced/Eco process input at 60 Hz but
skip duplicate blits and swaps between visualizer frames; visible UI animation
has a mode-rate deadline and actions request immediate presentation. Visualizer
Off stops the main projectM and preset timers, presents a solid background, and
updates hidden-UI output at 4 Hz. The presets page can still render previews.

Preview creates its projectM instance lazily, throttles to the smaller of 25 FPS
and the selected mode's FPS, and reuses the UI-owned 120 ms selection delay before loading
its shader. Resize preserves pending selections. The instance remains allocated
until shutdown to avoid repeated driver initialization when reopening the page.

App owns a bounded, session-only cache of 256 preset profiles, separated by name,
mode and drawable size, and validated against the loaded preset's SHA-256 digest.
Thirty consecutive stable visualization frames establish a reusable resolution.
Soft-cut frames do not count. Known-heavy presets are omitted from random rotation
when alternatives exist; manual selection remains available. Profiles never
survive a process restart, so device/driver updates cannot reuse old measurements.

UI shader attribute/uniform locations are queried at link time and released with
their program. GL calls remain in the binding/rendering modules.


Первое открытие меню сохраняется в `internal/config` как `ui.menu_opened`.
App фиксирует это после действия открытия меню и сохраняет настройки; UI владеет
только таймером и отдельной экранной текстурой стартовой подсказки. Истечение
таймера не меняет сохранённую отметку. Help и footer разрешают названия кнопок
через общий UI controlLabel; исходное отображение SDL actions не изменяется.

Сведения об устройстве принадлежат UI и собираются лениво при первом открытии
темы Device. Сначала используются экспортированные PortMaster-переменные, затем
read-only fallback по известным CFW-файлам, device tree, `/proc`, SDL и OpenGL.
Результат кешируется до завершения процесса; shell-команды и периодический опрос
не используются. App добавляет к снимку сведения активного аудиобэкенда.

Числа треков и директорий на странице Catalogs являются только UI-проекцией
immutable metadata provider-specific shuffle indexes. Открытие темы лениво
запускает обычную сборку индекса, если shuffle был выключен при старте; Help не
обходит каталоги и не владеет их данными.
