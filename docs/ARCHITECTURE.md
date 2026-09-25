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
| Analog-stick response | `internal/input.StickDrive` | left-stick list scroll, right-stick seek |
| Playback queue | `internal/app` playback state | player commands, overlay snapshot |
| Удаление файлов | `internal/app/delete_service.go` | confirmation UI, rescan |
| Favorites | `internal/player` (`favorites.go`) | overlay navigation, input actions, JSON storage |
| Outbound Go HTTP transport and policy | `internal/util` | catalogs, connectivity probe, Radio Browser and ICY metadata |
| Remote catalogs | `internal/modland`, `internal/modarchive` | provider navigation and downloads |
| Internet radio directory and stream metadata | `internal/radio` | Radio navigation, station cache, player URL resolution, ICY panel data |
| Provider-neutral directory cache | `internal/catalog` | provider loaders, shuffle selection, UI listings |
| Downloaded track cache policy | `internal/player.TrackCache` | `internal/config`, downloader wiring, catalog UI |

Радиопоток разделён на два слоя: `internal/radio.Client.OpenStream` владеет
Go HTTP/ICY-транспортом и отдаёт очищенный от ICY metadata поток байт, а
`internal/player` передаёт его в отдельный FFmpeg decoder worker. Native
`FfmpegStream` держит bounded compressed/PCM buffers; SoLoud audio callback
читает только готовый PCM и не выполняет сеть, DNS, reconnect или блокирующее
декодирование. Базовые HLS master/media playlists и TS/fMP4 segments также
собираются в `internal/radio` до передачи в тот же decoder pipeline.

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
`settings.track_cache.max_bytes`; активный трек и все треки из любой Favorites
категории защищены от автоматического и явного удаления загрузки. Чтобы удалить
такой файл, пользователь сначала снимает отметку Favorite.
Первый startup-prune запускается в фоне после входа в render loop, поэтому
сканирование большого кэша не блокирует инициализацию приложения. После
завершения фоновой задачи приложение публикует готовую офлайн-проекцию одним
атомарным snapshot, поэтому shuffle не видит наполовину обновлённое состояние.
Файл `.cache/shuffle/cached-tracks.json` — восстанавливаемый индекс исходных
virtual paths, а не источник истины: при старте и после Download/Delete/Prune
он сверяется с непустыми файлами внутри разрешённых cache roots.
Корень Library показывает эту же проекцию как виртуальную папку Downloads;
UI получает только стабильный snapshot `TrackCache.CachedVirtualPaths()` и не
сканирует cache directories самостоятельно.

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
shuffle pool.

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

Основной цикл обрабатывает ввод и UI с частотой до 60 Гц (или с частотой дисплея
для `Max`), а главный projectM планируется независимо. Настройка `FrameRate` задаёт
фиксированный cap из доступных значений ниже частоты дисплея либо `Max`, равный
активной частоте дисплея; числовой cap, совпадающий с `Max`, в UI не показывается.
Рендеринг и UI остаются на закреплённом main thread; Windows shader compiler
использует отдельный закреплённый поток со своим shared GL-контекстом и скрытым
окном (отдельный drawable). Между кадрами визуализации
выводится последняя захваченная текстура. Показатель `FPS` остаётся телеметрией,
а adaptive policy учитывает wall-clock стоимость полного кадра и фактические
интервалы его вывода, исключая намеренные паузы адаптации.

Смена preset использует staged pipeline. `internal/app` читает архив в worker с
`context`/`requestID`, поэтому новый выбор отменяет устаревший результат, а
активный preset не заменяется до успешного native commit. Подготовка projectM
сохраняет parsing preset, выбор текстур и создание GL-ресурсов на main thread.
На Windows `internal/app.shaderCompiler` владеет отдельным скрытым SDL-окном,
shared GL-контекстом и закреплённым worker thread. Worker переводит независимые
MilkDrop HLSL-шейдеры в GLSL и компилирует/линкует их даже без KHR/ARB extensions;
после `glFinish` он публикует результат через release/acquire. Для подготовки
шейдера worker получает только копии строк и enum-значения: он не читает preset,
texture descriptors или активное render state. Main thread проверяет готовность
и принимает program ID. Ошибка пользовательского warp сохраняет прежний fallback
на стандартный warp, а ошибка пользовательского composite запускает стандартный
composite через ту же очередь.
Очередь хранит weak references, удаляет отменённые jobs и никогда не заставляет UI
ждать текущую CPU-трансляцию или компиляцию.
Закрытие приложения будит очередь, дожидается worker и уничтожает его контекст
до удаления последнего основного контекста. Ошибка создания/проверки sharing
на Windows явно прекращает запуск, а не включает незаметно блокирующий fallback.

На других платформах сохраняется KHR/ARB polling при наличии расширения и
синхронный fallback без него. Готовность каждого pending preset проверяется
по его собственному списку shaders: незавершённый preview не блокирует commit
основного preset. Preview также использует Begin/Poll/Commit и сохраняет активную
картинку до успешной подготовки нового выбора. До commit рендерится прежняя
визуализация; после commit запускается обычный smooth transition. Автоматический
переход, пришедший во время загрузки, ставится в очередь. Native create/init,
выбранный compile mode и main-thread commit имеют отдельные замеры в логе.
Shared context не гарантирует отсутствия внутренних глобальных блокировок драйвера;
Windows/Intel acceptance и остаточные FPS-замеры описаны в архивном отчёте
[WINDOWS-PRESET-FREEZES.md](archive/WINDOWS-PRESET-FREEZES.md).

При `Adaptive=true` выбранная в настройках resolution является верхним пределом.
Разрешение временно уменьшается или повышается по результатам cadence, но никогда
не поднимается выше сохранённого значения. При `Adaptive=false` выбранная resolution
используется напрямую.
В верхней строке overlay показывается фактическая высота текущего render target,
поэтому это значение может быть ниже выбранного верхнего предела во время adaptive.

Adaptive использует целевую частоту как единственный критерий качества кадра.
Активная стоимость кадра включает render/capture и последующий blit/overlay/swap. Ожидание
планировщика между кадрами не входит в стоимость. Swap может ждать vsync и
отложенную GPU-работу; измерение wall-clock не выдаётся за GPU execution time.
Отдельное окно измеряет интервалы между выводами кадров. Обе метрики содержат
10 измерений; после сброса нужны 11 кадров для 10 полных интервалов.
Бюджет равен периоду текущей назначенной частоты. Устойчивый средний интервал
больше 110% назначенного периода разрешает пробу снижения resolution, но сама
адаптация никогда не меняет выбранную частоту кадров.
После пробного снижения новое полное окно проверяет выигрыш: достаточно
уменьшения полной стоимости либо интервала на 8%, либо восстановления cadence
в пределах 110% периода. Если первое окно не доказывает пользу, запрашивается
второе независимое окно без изменения FPS/resolution. Только повторное отсутствие
пользы возвращает resolution и запрещает дальнейшее снижение до нового пресета.
Пределом является нижняя ступень общей сетки. Глобальная сетка: 1, 3/4, 5/8, 1/2, 3/8, 1/4, 3/16, 1/8
размера экрана (720, 540, 450, 360, 270, 180, 135, 90p на экране 720p).
Близкие ступени 2/5 и 1/5 убраны, чтобы проба давала измеримый выигрыш. Используются только
целочисленные размеры; граница округляется к большей доступной ступени.
После достижения нижней ступени фактический FPS может быть ниже цели на тяжёлом
пресете, но сохранённая настройка частоты не изменяется. Восстановление разрешения
разрешено после устойчивой cadence в пределах 110% при стоимости кадра не выше
полного бюджета и всегда ограничено исходным ceiling.
Повышение resolution требует нижнего порога; неудачная
проба откатывается и блокируется до нового пресета. Снижение требует секунды
устойчивого отставания, восстановление — 3 секунды. Между изменениями собираются
свежие измерения. Новый или неизвестный пресет сохраняет текущую эффективную
ступень; известный может выбрать только такую же или более низкую ступень из
session cache. Повышение выполняется последующими измерениями adaptive policy.
Выбранная пользователем частота кадров всегда сохраняется.
Измерения перехода не попадают в policy. Взаимодействие с UI не сбрасывает
изученную чувствительность пресета к resolution.

Удержание клавиши или крестовины навигации рассчитывает шаги по монотонному
времени, а не по числу кадров. Если projectM задержал основной цикл, следующий
проход может обработать несколько накопившихся шагов с ограниченным бюджетом
работы. Вертикальный левый стик прокручивает список со скоростью, зависящей от
отклонения. Он и правый стик для перемотки используют общий
`internal/input.StickDrive`: удержание не менее 90% отклонения 400 мс запускает
удвоение скорости раз в секунду с пределом 8×. Навигация применяет эту скорость
в шагах списка в секунду; перемотка — в секундах аудио в секунду.
Вне активного взаимодействия расписание и качество фонового визуализатора не
меняются. Во время взаимодействия сначала показывается обновлённый UI поверх
последней текстуры, затем планируется следующий кадр projectM на том же GL-потоке.
`internal/app.interactionBudget` владеет временным ограничением частоты: период
равен максимуму периода выбранной частоты и четырёх оценочных стоимостей кадра, с потолком
125 мс для добавленного ограничения. Это оставляет примерно 75% времени остальному
циклу, пока стоимость кадра позволяет. Оценка учитывает render/capture и превышение
бюджета UI при blit/draw/swap; растёт сразу, снижается постепенно. Основной фон и
preview имеют независимые оценки. Непрерывный ввод не продлевает deadline рендера;
после превышения периода дорогим native-вызовом оставляется один UI-only tick,
без догоняющих кадров. 125 мс ограничивает ожидание планировщика, но не длительность
непрерываемого GL-вызова и не гарантирует минимальный FPS на любом пресете.
После 200 мс без ввода обычное расписание возобновляется. Разрешение и сохранённые
настройки не меняются. Намеренно ограниченные кадры учитываются в отображаемом FPS,
но исключаются из отдельного измерителя adaptive resolution и оценки тяжести
пресета; после взаимодействия измерения адаптации собираются заново. При `Transparency=0` визуализатор, blit
фоновой текстуры и feedback-инъекция полностью пропускаются, пока UI открыт;
исключение — страница пресетов: её thumbnail-preview продолжает работать, чтобы
просмотр выбранного пресета оставался доступным при непрозрачном меню.

Во время плавной смены пресета adaptive resolution приостанавливается на
длительность projectM soft cut. При запуске перехода история adaptive policy
очищается, но текущий render target сохраняется; после перехода решение снова
принимается только по новым завершённым кадрам визуализации. Так временная
стоимость смешивания двух пресетов не повышает разрешение перед измерением и не
понижает постоянное render resolution из-за самого перехода.
Sampler projectM soft cut использует тот же выбранный пользователем фильтр
масштабирования: `Bilinear (smooth)` сглаживает изображение, а
`Nearest neighbor (crisp pixels)` сохраняет чёткие пиксельные границы.
Изменение настройки обновляет и уже выполняющийся переход.

Если пробный upscale превышает 95% верхнего utilization-порога или нарушает
cadence (средний интервал больше 110% назначенного периода) и требует возврата,
эта верхняя ступень больше не пробуется для текущего пресета. Новый пресет или
переконфигурация adaptive resolution снимает ограничение.

## Проверки перед изменением

Портативный архив `dist/presets.gsa` содержит только пресеты из benchmark CSV
с результатом не ниже 20 FPS. Перед упаковкой Makefile проверяет его индекс;
устаревший архив пересобирается, поэтому уже существующий файл не обходит
фильтрацию.

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
циклическую навигацию. При отсутствии подтверждённого подключения shuffle
работает через атомарную offline-проекцию: полный local index плюс только
фактически существующие скачанные подмножества Modland и ModArchive. Shuffle
All взвешен по числу доступных треков, а Shuffle Source остаётся привязанным к
текущему источнику и выбирает только его кэшированные треки. Кэшированные
каталоги разрешают offline-навигацию, но не подтверждают сеть.
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

Adaptive resolution применяет выбранный пользователем ceiling при сбросе, resize
и повышении. `FrameRate` задаёт фиксированный cap ниже активной частоты дисплея
или `Max` (активная частота дисплея), а `Adaptive` отдельно разрешает изменение
resolution. Hysteresis
измеряется wall-clock временем; пробные изменения проверяются по независимым
полным окнам кадров.


## Handheld rendering and memory budgets

`internal/prof` reads available device memory, `internal/player` selects a
bounded tracker pre-render budget, and app wires the result at audio startup.
Settings owns the persisted visualizer toggle, frame rate, adaptive-resolution
toggle, and configured resolution ceiling. The app derives the effective
frequency and resolution from those independent values; the player serializes native loads and owns the PCM budget
calculation described in SEEK-DESIGN.md.

The UI loop remains responsive while the visualizer follows the selected frame
rate and skips duplicate blits and swaps between visualizer frames. Visible UI
animation has its own deadline and actions request immediate presentation. Visualizer
Off stops the main projectM and preset timers, presents a solid background, and
updates hidden-UI output at 4 Hz. The presets page can still render previews.

Preview creates its projectM instance lazily, follows the selected frame rate
up to its 25 FPS preview cap, and reuses the UI-owned 120 ms selection delay before loading
its shader. Resize preserves pending selections. The instance remains allocated
until shutdown to avoid repeated driver initialization when reopening the page.

App owns a bounded, session-only cache of 256 preset profiles, separated by name,
frame rate, resolved target FPS, adaptive toggle and drawable size, and validated
against the loaded preset's SHA-256 digest.
Thirty consecutive stable visualization frames establish a reusable resolution.
Soft-cut frames do not count. Automatic skipping of a preset that remains too
heavy at the minimum resolution is temporarily disabled; reaching the minimum
resolution only leaves the visualizer at that quality. Profiles never
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

Числа треков и директорий на странице Sources являются только UI-проекцией
immutable metadata provider-specific shuffle indexes. Открытие темы лениво
запускает обычную сборку индекса, если shuffle был выключен при старте; Help не
обходит каталоги и не владеет их данными.

Событие крестика SDL (`WINDOWEVENT_CLOSE`) проверяется по ID главного окна
в `internal/app.run_loop.go` и завершает цикл явно. Это нужно в том числе потому,
что окно worker-контекста projectM скрыто, но остаётся окном SDL; полагаться на
`SDL_QUIT` после закрытия главного окна нельзя.

`FrameRate=Max` follows the current SDL display refresh rate (60 Hz fallback),
checked once per second for display moves/display-mode changes. The settings list
is rebuilt from that active refresh and preserves a numeric 60 FPS choice on
displays above 60 Hz. A refresh change resets adaptive measurements while retaining
the current effective render step. With adaptive resolution disabled the configured
resolution is fixed; when enabled it is the ceiling for bounded reductions.
Adaptive changes the image size only and never changes the user's selected frame-rate target.

На странице Presets каждый вывод UI начинается с привязки и очистки экранного
framebuffer, даже если превью пропустило кадр по таймеру. Привязка и инъекция
в feedback главного projectM на этой странице запрещены: главный визуализатор
приостановлен, а обновление текста не зависит от частоты превью.

## Radio and network execution boundaries

All Go HTTP requests use `internal/util.Get`: connect/TLS timeout 5 s,
response headers 15 s, at most three attempts, and 30 s per outstanding body
read. The body deadline excludes consumer pauses and imposes no maximum stream
lifetime. Closing a response cancels its request, including reconnect work.
Directory requests have tighter operation deadlines.

Списки Radio Browser кэшируются на 30 дней. В кэше хранится только запрошенная
часть каталога: первая страница загружается сразу, следующие страницы
автоматически дозагружаются в фоне, когда курсор подходит к концу списка, и
объединяются по UUID. Запросы
`random` выполняются с `limit=1`, сразу запускают найденную станцию и никогда
не сохраняются в directory cache.
Последний выбранный каталог и фильтр сохраняются вместе с очередью текущей
станции, поэтому после перезапуска меню восстанавливает полный путь до её
списка; если каталог недоступен, показывается сама станция как fallback.

Radio loading owns a cancellable stream context, forwarded from the load request
until prebuffer completes. It is then owned by the source, not the completed load
worker. Stop/switch detaches the SoLoud voice and queues stream destruction off
the UI thread; Player.Close waits for load, producer, watchdog and destruction
workers. Only shutdown waits for native decoder joins. The PCM buffer is a
preallocated SPSC ring (4 seconds stereo, 44.1 kHz); the audio callback uses only
atomic indices and sample copies. Compressed input is capped at 2 MiB. A paused
consumer applying backpressure is not classified as a network stall.

Radio Browser listings, click notifications and favicon downloads run in workers.
At most two favicon jobs run concurrently; decoding validates a 2 MP source
budget, prepares a bitmap no larger than 256×256 off the GL thread, and the UI
retains at most 32 prepared images. GL upload stays on the main thread.
ModArchive cache misses also run in a cancellable worker with request IDs;
navigation renders a loading entry and never calls the synchronous network API.

A radio stream ending after startup enters bounded failure recovery, including
under Repeat One. Live sources cannot seek or use the tracker restart fallback.
When connectivity is confirmed, a failed station is removed from its query,
imported descriptor and last-queue caches; when the device is offline, the
descriptor remains available for a later retry. Cache writes execute in
submission order without waiting on disk from the UI.

Basic HLS supports master/media playlists, sequence-based deduplication, redirects,
and TS/fMP4 segment transport. Encrypted streams, byte ranges, gaps,
discontinuities and changing initialization segments return explicit errors.
Partial segment transfers are never replayed into the same decoder. See
[archived radio review](archive/RADIO-REVIEW.md) for implementation-time
verification notes; remaining platform checks are in
[PORTMASTER-RELEASE.md](PORTMASTER-RELEASE.md).
