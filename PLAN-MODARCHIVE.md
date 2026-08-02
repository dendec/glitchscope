# План интеграции ModArchive (http://modarchive.textfiles.com/)

Этот документ содержит план интеграции каталога **ModArchive** в аудиоплеер PMV.

---

## 1. Концепция и структура каталога

Интеграция работает через живой HTTP-браузинг каталогов зеркала `http://modarchive.textfiles.com/`.

В отличие от Modland, предзагруженный оффлайн-список файлов (каталог) не требуется, так как ModArchive содержит только распространенные трекерные форматы, полностью поддерживаемые PMV (MOD, XM, IT, S3M и др.).

### Иерархия навигации (ModArchive):
1. **Корневой уровень ModArchive**: Года добавления (`2008 Additions` ... `2023 Additions`).
   - *Примечание:* 2007 snapshot (`modarchive_2007_official_snapshot_120000_modules`) имеет другую структуру и исключен из основного ModArchive (зарезервирован под будущий `modarchive-old`).
2. **Уровень форматов**: По типам треков (`MOD`, `IT`, `XM`, `S3M`, `AHX`, `MED`, `MO3`, `MTM` и т.д.).
3. **Алфавитный уровень**: Подпапки по первой букве/цифре (`1_9`, `A`, `B`, ..., `Z`).
4. **Уровень треков**: Список модулей (напр. `a_little_bit_o_fun.mod.zip`).

---

## 2. Архитектура компонента `internal/modarchive`

### HTML Parser (`modarchive.go`)
- Функция `ParseDirectoryListing(htmlBody, baseURL string) ([]DirItem, error)` для парсинга стандартного HTML-индекса директорий Apache/Nginx.
- Фильтрация служебных ссылок (`Parent Directory`, `README`, `.zip` архивов вне списков и т.д.).
- Преобразование технических имен папок в понятные названия (например, `modarchive_2023_additions` → `2023 Additions`).

### HTTP-кэширование списков
- Индексы загруженных папок кэшируются на диске в `modarchive-cache/index/` в виде JSON, что обеспечивает мгновенную повторную навигацию и работу offline для ранее открытых папок.

### Загрузка и распаковка файлов (`download.go`)
- `DownloadAndExtract(baseDir, remoteURL string, onProgress func(read, total int64)) (string, error)`:
  1. Проверка наличия распекованного модуля в `modarchive-cache/files/`.
  2. Скачивание архива/файла с зеркала `http://modarchive.textfiles.com/...`.
  3. Если скачанный файл является ZIP-архивом (например `.mod.zip`), автоматическая распаковка трека во внутренний кэш `modarchive-cache/files/`.
  4. Возврат локального пути к модулю для проигрывателя SoLoud/libopenmpt.

---

## 3. Интеграция с UI и Плеером

- **Плеер (`internal/player/library.go`, `internal/app/app.go`)**:
  - Константа `player.ModArchivePrefix = "modarchive:"` и утилита `IsModArchive(path)`.
  - При воспроизведении трека с префиксом `modarchive:` запускается фоновое скачивание/распаковка с отображением прогресса в UI (аналогично Modland).
- **Интерфейс (`internal/ui/overlay_nav.go`, `overlay_input.go`)**:
  - Пункт "ModArchive" на корневом экране библиотеки рядом с "Modland".
  - Динамические уровни навигации (`entryModArchiveRoot`, `entryModArchiveDir`, `entryModArchiveTrack`).
  - Отображение индикатора загрузки при обращении к сети.

---

## 4. План проверки

1. **Модульные тесты (`go test ./internal/modarchive/...`)**:
   - Тестирование HTML-парсера на реальных HTML-страницах `textfiles.com`.
   - Тестирование логики скачивания и распаковки ZIP-файлов.
2. **Ручная проверка**:
   - Сборка и запуск `make build` -> `./dist/pmv`.
   - Переход в меню: `ModArchive` -> `2023 Additions` -> `MOD` -> `A` -> запуск трека.
   - Проверка индикатора скачивания, разархивации и успешности воспроизведения.
