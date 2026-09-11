# Радио: ревью перед релизом

Проверен рабочий diff добавления радио и общих HTTP-вызовов. Исправления
включают существующий синхронный fallback ModArchive, поскольку он нарушал
требование не выполнять сетевые операции из потока интерфейса.

## Исправленные дефекты

| Приоритет | Проблема | Исправление |
|---|---|---|
| P1 | `WithoutCancel` отключал отмену открытия/чтения радиопотока | Context сохраняется до источника; старт ограничен 20 с; Stop отменяет незавершённую загрузку |
| P1 | Stop/смена станции ждали HTTP Close и native thread join | Отсоединённый источник уничтожается в фоне; Close плеера ждёт все worker'ы |
| P1 | Запись держала общий mutex до 100 мс; callback ждал PCM mutex и освобождал deque blocks | Неблокирующая запись compressed bytes и фиксированный PCM ring с atomic indices |
| P1 | `mChannels` скрывал поле AudioSource, источник объявлялся моно | Используется поле базового класса; native тест проверяет два противоположных стереоканала |
| P1 | Освобождался AVIOContext, но не его заменяемый буфер | Буфер освобождается до AVIOContext; teardown проверен ASan/UBSan |
| P1 | ModArchive при cache miss вызывал HTTP непосредственно из навигации | Отменяемый worker, loading row, request ID и применение результата из UI |
| P1 | Не было общего предела зависшего чтения HTTP body | 30 с на отдельный Read с отменой запроса; consumer pause не расходует timeout |
| P1 | Favicon decode и обход всех пикселей выполнялись во время render; размеры не проверялись | DecodeConfig/pixel budget, обработка в worker, bounded bitmap/cache/concurrency |
| P2 | Успевший завершиться decoder с достаточным prebuffer зависал до timeout | Состояние ended с готовым PCM допускается к воспроизведению |
| P2 | Пауза заполняла compressed buffer и вызывала ложный idle timeout | Ожидание свободного буфера учитывается как backpressure; контролируется также отсутствие PCM |
| P2 | Worker'ы потока и click requests не были полностью учтены при shutdown | WaitGroup, отмена и определённый порядок teardown; click только после успешного старта |
| P2 | Runtime EOF радио попадал в обычный Repeat One; seek пытался перезапустить live source | Ошибка идёт в bounded recovery; live seek/restore-offset отключены |
| P2 | Ошибка станции удаляла URL, делая ручной retry/Favorites невозможными | Descriptor сохраняется; imported descriptors сохраняются независимо от текущего source |
| P2 | HLS dedup по URL терял сегменты с повторяющимися URL; относительные пути ломались после redirect | Media sequence и URL конечного playlist response |
| P2 | HLS частично передавал сегмент, затем повторял его; unsupported tags молча игнорировались | Частичная ошибка завершает источник; неподдерживаемые варианты явно отклоняются |
| P2 | HLS variant cycle мог бесконечно перезапрашиваться; init-map рос без лимита | Глубина master ограничена; init-map заменён одним состоянием |
| P2 | Пустой успешный каталог оставался в состоянии Loading | Наличие snapshot отделено от его длины; добавлена локализованная пустая строка |
| P2 | Async cache writes могли сохранить устаревший snapshot после нового | Записи выполняются в порядке постановки |

## Проверки

- `scripts/dtest.sh test`: все Go-пакеты.
- `scripts/dtest.sh lint`: форматирование и статический анализ.
- `scripts/dtest.sh race`: player, radio, util, ui, app, modarchive.
- `scripts/dtest.sh native-radio`: native decoder с ASan/UBSan; stereo,
  underrun, ring wrap-around/backpressure, полный drain, abort при ожидании IO.
- `make dist`: сборка и упаковка amd64 через актуальный builder.

HTTP-тесты используют локальные серверы: зависшие headers/body, отмена
reconnect через Close, HLS redirect/sequence/unsupported/cycle. UI-тест проверяет,
что cache miss возвращает loading row, пока сервер не отвечает. Player-тесты
проверяют отмену открытия и prebuffer, завершившийся короткий поток и быстрый
Stop при намеренно заблокированном teardown.

## Границы готовности

Проверки подтверждают отсутствие HTTP/DNS/read/join на UI-пути радио и
сетевом fallback навигации. Это не измерение frame time на реальном handheld:
драйвер, GL upload, локальная файловая система и шейдеры могут задерживать кадры.
Перед выпуском на устройство нужен smoke/soak-прогон с потерей Wi-Fi,
длительной паузой и переключениями реальных станций; ARM64 runtime здесь
не проверялся.

HLS не заявляется полным: encrypted/byte-range/discontinuity/gap streams и
смена init segment отклоняются. Выбирается первый master variant; отдельные
renditions и адаптивный bitrate не реализованы. Реальные AAC/MP3/Ogg станции
могут различаться поведением контейнера при reconnect; совместимость со всеми
провайдерами автоматические тесты не гарантируют.

Освобождение custom AVIO сверено с [примером FFmpeg](https://ffmpeg.org/doxygen/trunk/avio_read_callback_8c-example.html),
media sequence и ограничения HLS — с [RFC 8216](https://www.rfc-editor.org/rfc/rfc8216).
