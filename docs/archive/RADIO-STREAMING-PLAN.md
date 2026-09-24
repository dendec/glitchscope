# Радио: неблокирующий сетевой и аудиопоток

> Исторический план реализации. Потоковый pipeline и базовая поддержка HLS
> уже реализованы. Текущее поведение и ограничения описаны в
> [RADIO-STREAMING.md](../RADIO-STREAMING.md); проверки на реальных станциях и
> PortMaster перечислены в [PORTMASTER-RELEASE.md](../PORTMASTER-RELEASE.md).

Статус: выполнено ревью сетевых границ и жизненного цикла; добавлены
регрессионные тесты Go, race и native ASan/UBSan. Поддержка HLS ограничена
перечисленными ниже вариантами; совместимость с реальными станциями и
поведение на handheld требуют отдельного smoke/soak-прогона.
Отчёт о ревью: [RADIO-REVIEW.md](RADIO-REVIEW.md).

## Цель

Сетевые сбои радиостанции не должны останавливать главный цикл, навигацию,
визуализатор или аудиомикшер. FFmpeg должен заниматься только разбором
контейнера и декодированием, а HTTP-транспорт должен принадлежать Go-слою.

## Целевая схема

```text
Go HTTP worker
  -> bounded compressed ring buffer
  -> FFmpeg decoder worker
  -> bounded PCM ring buffer
  -> SoLoud audio callback
```

Главный поток и `SoLoud.getAudio()` не должны выполнять DNS, HTTP, reconnect,
ожидание body, открытие FFmpeg URL или блокирующее декодирование. При временной
нехватке данных аудиоколбэк получает тишину/underrun, а не блокируется.

## Этапы реализации

### 1. Наблюдаемость и базовые границы

- Разделить логи на HTTP request, response headers, prebuffer, decoder ready,
  first PCM, playback started, underrun и stream error.
- Добавить проверку на зависший connect и зависший response body.
- Зафиксировать, что UI продолжает обрабатываться во время каждого сценария.

### 2. Сетевой транспорт

Создать потоковый transport в Go на базе общего HTTP-клиента. Он отвечает за:

- `GlitchScope/1.0`, redirects, таймауты и reconnect;
- отмену через `context.Context`;
- ограниченный буфер сжатых данных;
- ICY metadata и передачу событий приложению;
- ошибки и жизненный цикл соединения.

Сетевой worker должен завершаться при смене станции или закрытии плеера.

### 3. Отдельный FFmpeg decoder worker

- Передавать в FFmpeg поток байт через custom IO, без URL и сетевых протоколов.
- Выполнять `avformat_open_input`, `av_read_frame` и декодирование вне
  SoLoud audio callback.
- Записывать готовый PCM в ограниченный ring buffer.
- Не допускать роста памяти при зависшем или слишком быстром источнике.

### 4. Интеграция с SoLoud и Player

- Радио публикуется только после минимального prebuffer, например 0,5–2 с.
- `getAudio()` только читает PCM и никогда не ждёт сеть или decoder worker.
- При underrun отдаётся тишина; повторяющиеся underrun переводят поток в
  ошибку и используют существующее переключение на следующую станцию.
- Сохранить request ID, отмену устаревших загрузок и единое обновление
  текущей станции/курсора/UI.

### 5. HLS

Для `.m3u8` реализован отдельный Go transport:

- загрузка и обновление playlist;
- получение TS/fMP4-сегментов;
- media sequence без растущего seen-set;
- передача TS/fMP4-сегментов и `EXT-X-MAP` в тот же decoder pipeline.

Шифрование, byte ranges, gaps, discontinuity и смена init segment пока
отклоняются с ошибкой. Master выбирает первый вариант; отдельного выбора
bitrate/audio rendition нет. При частично переданном сегменте источник
завершается: повтор тех же байт в существующий decoder запрещён.

Обычные MP3/AAC/Ogg-потоки переводятся первыми; HLS нельзя заменить простым
`GET` плейлиста.

### 6. Удаление старого URL-пути

- `NewFfmpegURL` и `openURLInput` удалены; FFmpeg больше не открывает URL.
- Удалить двойное открытие радиостанции.
- Оставить FFmpeg для локальных файлов и уже подготовленного входного потока.

## Проверки

Нужны тесты на:

- зависший connect и зависший body;
- отмену и смену станции;
- reconnect и bounded buffers;
- ICY metadata;
- underrun и переход на следующую станцию;
- устаревшие результаты;
- HLS playlist/segments;
- отсутствие блокировки UI при сетевой ошибке.

Перед завершением каждого этапа запускать `scripts/dtest.sh test` и
`scripts/dtest.sh lint`.

## Текущее выполнение

Уже реализовано для прямых HTTP-аудиопотоков:

- один Go HTTP/ICY connection вместо отдельного metadata connection;
- FFmpeg custom IO без URL/network protocol layer;
- отдельный native decoder worker;
- bounded compressed и PCM buffers;
- 500 ms prebuffer и таймаут старта;
- idle timeout body с отменой worker’ов;
- SoLoud callback читает только готовый PCM.
- базовый HLS transport для master/media playlists и TS/fMP4 segments.

Прямые HTTP-аудиопотоки и базовый HLS больше не используют legacy URL-путь.

## Критерии готовности

- Главный цикл продолжает работать при не отвечающем сервере.
- В аудиоколбэке отсутствуют сетевые операции и ожидание decoder worker.
- Радиостанция подключается только один раз.
- Ошибка потока приводит к обычному recovery flow с правильным курсором,
  правой панелью и нижней строкой now playing.
