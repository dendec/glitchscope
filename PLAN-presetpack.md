# PMV — Portable Music Visualizer archive

## 1. Формат файла `.pmv`

Бинарный контейнер. Little-endian.

```
[4]byte  magic  "PMV\x00"
[4]byte  version (1)
[4]byte  num_entries
── index ──
[num_entries]:
  [2]byte  key_len
  [key_len]byte key     // относительный путь: "Dancer/Aurora/$$$ Royal.milk"
  [4]byte  zst_len
── zstd blocks ──
[num_entries]:
  [zst_len]byte raw  // zstd-сжатый .milk файл
```

**Размер:** ~17MB (заголовок 12B + индекс ~660KB + zstd ~16MB).

## 2. Сборка — `cmd/pmv-pack/main.go`

Go-тулза, без CGo, чистая реиспользуемая зависимость `github.com/klauspost/compress/zstd`.

```
Usage: pmv-pack <presets|textures> <input-dir> <output.pmv>
```

Логика:
1. `filepath.WalkDir` собирает `.milk` или texture-файлы в зависимости от режима
2. Сортировка по ключу (детерминизм)
3. `zstd.EncodeAll` — каждый файл независимо
4. Запись `.pmv`: header → index → data blocks

### Makefile

```makefile
PMV_FILE := dist/presets.pmv
pmv: presets
    go run ./cmd/pmv-pack presets $(PRESETS_DIR) $(PMV_FILE)
```

## 3. Загрузка — `internal/presets/presets.go`

### API

| Функция | Назначение |
|---------|-----------|
| `Open(dir string) error` | Читает `presets.pmv` из `dir`, сканирует `dir/*.milk` + `dir/*/*.milk`. User-файлы перезаписывают архив. |
| `Names() []string` | Отсортированный список ключей (merged). |
| `Read(key string) ([]byte, error)` | User-файл приоритетнее, иначе из presets.pmv. |
| `DefaultPreset() []byte` | Хардкод `wave_r=1`. |

### Структура

```go
type presetStore struct {
    dir     string
    pmvFile *archive.Archive  // presets.pmv
    entries map[string]entry  // ключ → filesystem или archive
    names   []string
}
```

### Read flow

1. Ищем `key` в `entries` map
2. `dataOff < 0` → `os.ReadFile(dir/key)`
3. Иначе → `pmvFile.Read(key)`

## 4. Интеграция в main.go & deploy

- `presets.Open(presetDir)` заменил старый `presets.SetExternalDir()` + ReadDir loop
- Старое поле `externalDir` удалено
- `deploy` пушит `presets/presets.pmv` и `presets/textures.pmv` на устройство
- `dist-portmaster` копирует оба `.pmv` в zip-архив

## 5. Edge cases

| Сценарий | Поведение |
|----------|-----------|
| Нет `presets/` | Open вернёт ошибку, Names() nil → DefaultPreset |
| `presets/` пустая | Open успех, Names() nil → DefaultPreset |
| Только `presets.pmv` | Open загружает архив |
| Только `.milk` | Open сканирует файлы |
| `presets.pmv` + `.milk` | User-файлы перезаписывают архив |
| Битый `presets.pmv` | Закрываем файл, работаем только с user-файлами |

## 6. Архив текстур `textures.pmv`

Текстуры bundled отдельно от пресетов, потому что projectM принимает для них
каталог поиска, а не поток байтов:

```
[4]byte  magic  "PMV\x00"
[4]byte  version (1)
[4]byte  num_entries
── index ──
[num_entries]:
  [2]byte  name_len
  [name_len]byte name       // относительное имя файла, например "clouds.jpg"
  [4]byte  zst_len
── zstd blocks ──
[num_entries]:
  [zst_len]byte raw         // zstd-сжатые байты изображения
```

`portmaster/presets/textures.pmv` собирается целью `make texture-archive`. При запуске
приложение распаковывает его во временный каталог и передаёт этот каталог
projectM. Изображения пользователя ищутся в каталоге `presets/` и копируются
в тот же временный каталог после распаковки архива. Архив поставляется в
каталоге `presets/` рядом с `presets.pmv`, а не встраивается в бинарник.

Перед упаковкой `scripts/optimize-textures.sh` удаляет metadata и проверяет
каждое изображение с палитрами 256, 128, 64, 32, 16, 8, 4 и 2 цвета. Каждый
кандидат сравнивается с декодированным исходником по SSIM; при результате ниже
`TEXTURE_SSIM_THRESHOLD` (по умолчанию `0.9`) перебор для этой текстуры
прекращается. Самый маленький
подходящий PNG выбирается только если он меньше исходника без metadata; иначе
сохраняется исходный формат. Решения записываются в
`docs/texture-optimization.csv`.

`make texture-report` создаёт `docs/texture-usage.csv`: для каждой ссылки
записываются preset, sampler, нормализованный basename и тип (`file` или
`random`). Это используется для ручной проверки визуальных изменений после
оптимизации.
