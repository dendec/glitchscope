# MDP — MilkDrop Package format

## 1. Формат файла `.mdp`

Бинарный контейнер. Little-endian.

```
[4]byte  magic  "MDP\x00"
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

## 2. Сборка — `cmd/mdp-pack/main.go`

Go-тулза, без CGo, чистая реиспользуемая зависимость `github.com/klauspost/compress/zstd`.

```
Usage: mdp-pack <input-dir> <output.mdp>
```

Логика:
1. `filepath.WalkDir` собирает все `.milk` файлы
2. Сортировка по ключу (детерминизм)
3. `zstd.EncodeAll` — каждый файл независимо
4. Запись `.mdp`: header → index → data blocks

### Makefile

```makefile
MDP_FILE := dist/presets.mdp
mdp: presets
    go run ./cmd/mdp-pack $(PRESETS_DIR) $(MDP_FILE)
```

## 3. Загрузка — `internal/presets/presets.go`

### API

| Функция | Назначение |
|---------|-----------|
| `Open(dir string) error` | Читает `.mdp` из `dir/.mdp`, сканирует `dir/*.milk` + `dir/*/*.milk`. User-файлы перезаписывают .mdp. |
| `Names() []string` | Отсортированный список ключей (merged). |
| `Read(key string) ([]byte, error)` | User-файл приоритетнее, иначе из .mdp. |
| `DefaultPreset() []byte` | Хардкод `wave_r=1`. |

### Структура

```go
type presetStore struct {
    dir     string
    mdpFile *os.File          // открыт для ReadAt
    entries map[string]entry  // ключ → offset/zstLen (-1 = filesystem)
    names   []string
}
```

### Read flow

1. Ищем `key` в `entries` map
2. `dataOff < 0` → `os.ReadFile(dir/key)`
3. Иначе → `mdpFile.ReadAt(buf, dataOff)` → `zstdDec.DecodeAll(buf, nil)`

## 4. Интеграция в main.go & deploy

- `presets.Open(presetDir)` заменил старый `presets.SetExternalDir()` + ReadDir loop
- Старое поле `externalDir` удалено
- `deploy` пушит один `presets/.mdp` на устройство (без `tar` + распаковка)
- `dist-portmaster` копирует `.mdp` в zip-архив (вместо 10K файлов)

## 5. Edge cases

| Сценарий | Поведение |
|----------|-----------|
| Нет `presets/` | Open вернёт ошибку, Names() nil → DefaultPreset |
| `presets/` пустая | Open успех, Names() nil → DefaultPreset |
| Только `.mdp` | Open загружает архив |
| Только `.milk` | Open сканирует файлы |
| `.mdp` + `.milk` | User-файлы перезаписывают .mdp |
| Битый `.mdp` | Закрываем файл, работаем только с user-файлами |
