# Диагностика фризов при смене пресетов на Windows

**Статус:** исправление фризов при переключении пресетов и закрытии окна
подтверждено на Windows 11 / Intel UHD 600 логом 2026-09-22 13:08. Отдельный
симптом низкой частоты кадров зафиксирован в конце отчёта; он сам по себе не
доказывает регрессию от staged loading.

В логе `glitchscope-20260921-145513.log` (Intel UHD Graphics 600,
OpenGL 3.3, driver 31.0.101.2121) три смены пресета показывают:

| Request | Чтение архива | Native prepare | Ожидание после prepare до commit |
|---|---:|---:|---:|
| 1 | 5,5 мс | 4791 мс | 13,9 мс |
| 2 | 0,6 мс | 2590 мс | 13,5 мс |
| 3 | 1,2 мс | 4872 мс | 9,1 мс |

Основная задержка находится внутри `BeginPresetLoad`, который выполняется на
GL main thread. Перенос чтения архива в worker её не устраняет. Эти замеры
не позволяют отдельно оценить parsing, HLSL translation и вызовы GL driver.

Первоначальный async patch включал неблокирующие запросы только при наличии
`GL_KHR_parallel_shader_compile`. Драйвер, объявляющий лишь
`GL_ARB_parallel_shader_compile`, продолжал синхронно ждать компиляцию.
[ARB specification](https://registry.khronos.org/OpenGL/extensions/ARB/ARB_parallel_shader_compile.txt)
задаёт тот же completion token и механизм опроса, что и KHR.
Исправление распознаёт оба точных имени через indexed extension enumeration,
которая также работает в core profile. Windows сейчас запрашивает compatibility
profile; ошибка legacy enumeration в core profile не является доказанной
причиной задержки из данного лога.

В новом логе `OpenGL context` и `phase=gl_prepare_begin` содержат
`parallel_shader_compile=ARB|KHR|synchronous`. Это значение определяется тем же
native-кодом, который включает async path. Старый лог не содержал capability,
поэтому предположение об ARB-only драйвере требует проверки на исходном ПК.

## Повторный тест и shared-context worker

Лог `glitchscope-20260921-155104.log` подтвердил `parallel_shader_compile=synchronous`.
EXE на флешке побайтно совпал с первой исправленной сборкой (SHA-256
`ccfb5f454cca38fb2d4b71ffb841b1c3f91237d32899bd0f43bc55d6f2e8fcd7`).
Чтение занимало 2–2,5 мс, native prepare — 2530 и 2821 мс.
Гипотеза ARB-only не подтвердилась; расширения не решают проблему этого драйвера.

Новый путь выполняет shader compile/link в dedicated shared GL context на worker
thread. Для него создаётся отдельное скрытое SDL-окно: одновременно делать current
два контекста на одной EGL surface нельзя (`EGL_BAD_ACCESS`, пойман integration test).
Main thread продолжает рисовать старый preset. После завершения worker делает
`glFinish` в собственном контексте и публикует готовую program через атомарный флаг.
Отмена освобождает ссылки на jobs без ожидания native-вызова; очередь слабо
владеет jobs, поэтому устаревшие запросы пропускаются. Shutdown — единственное
место ожидания worker. Превью использует тот же staged pipeline.

`shader compiler ready mode=shared-context` подтверждает успешную настройку worker.
В `gl_prepare_begin` теперь отдельно записаны capability (`parallel_shader_compile`)
и фактический путь (`shader_compile_mode`). `synchronous` capability допустима,
если фактический путь — `shared-context`. Дополнительные замеры:

- `native_create_ms`: parsing и создание объектов preset;
- `native_initialize_ms`: выражения, HLSL translation, текстуры, submission;
- `shader_wait_us`: ожидание готовности jobs между итерациями main loop;
- `commit_us`: отдельная стоимость native commit на main thread.

## Проверка и условия снятия релизного блокера

- `make test`: native GL mocks с ASan/UBSan проверяют ARB/KHR/fallback,
  заблокированную компиляцию на worker, неблокирующий poll/cancel, выбрасывание
  устаревшей очереди, shader/link errors, очистку ресурсов и shutdown/restart.
  Go tests проверяют stale reader results и модель выбора preview.
- `scripts/dtest.sh gl`: software OpenGL integration test с реальными SDL-контекстами,
  двумя projectM instances, отменой preview, рендерингом старого preset и commit
  программ worker в основном контексте. Требуется актуальный builder image.
- `make lint`: Go-код и форматирование.
- На Windows 11 / UHD 600 проверить обычные переключения и быстрые повторные
  переключения, страницу Presets, возврат к активному preset и закрытие во время
  загрузки. Ввод и старая визуализация должны оставаться отзывчивыми во время
  компиляции. Проверить несколько ранее не загружавшихся presets, чтобы не
  ограничиваться тёплым shader cache.
- Сравнить main-thread `gl_prepare_us`/`commit_us` и видимые фризы с предыдущими
  логами. Только успешный тест на целевом ПК снимает релизный блокер.

Preset parsing, texture lookup и создание GL-ресурсов остаются на main thread.
Shared context не исключает глобальных блокировок внутри драйвера Intel.
Если фриз останется, фазовые замеры покажут, остаётся ли задержка в prepare/commit
или переехала в рендеринг во время worker compile. Linux/Mesa и mock tests
не заменяют Windows acceptance.


## Отчёт 2026-09-22: фризы и закрытие окна

Лог `glitchscope-20260922-110614.log` показывает, что EXE запустил
`shader compiler ready mode=shared-context`, но фризы остались. Два измеренных
preset имеют `native_create_ms` около 3–4 мс, `native_initialize_ms` 1471 и
2033 мс, `shader_wait_us` около 45–51 мс, `commit_us` около 3–4 мс.
Следовательно, компиляция шейдеров уже вынесена с главного потока. Остаточная
пауза находится в `Preset::Initialize`, но этот общий таймер ещё не показывает
конкретную операцию. Новый диагностический build отдельно записывает
`native_expressions_ms`, `native_framebuffers_ms`, `native_warp_shader_ms` и
`native_composite_shader_ms`; shader-времена включают texture lookup и CPU HLSL
transpile, но не последующую работу shared-context compiler. Эти замеры нужны,
чтобы выбрать безопасное следующее изменение, не перенося произвольные GL-вызовы
с current-context main thread.

Закрытие крестиком также было отдельной регрессией. Цикл обрабатывал `SDL_QUIT`,
но не `SDL_WINDOWEVENT_CLOSE`. Наличие скрытого SDL-окна shared GL worker означает,
что закрытие основного окна не обязано создавать глобальный quit event.
`handleFrameInput` теперь сравнивает ID события с ID основного окна и выходит
из цикла при его закрытии. Закрытие другого окна не завершает приложение.
Исправление покрыто unit test для SDL window event и входит в следующую Windows
сборку; закрытие с реального Windows 11 пока требует подтверждения пользователя.

## Диагностическая сборка 2026-09-22

Windows x64 пакет собран в `dist/windows-shader-worker-closefix/`. В нём есть
обработка крестика и четыре новых поля времени для фаз preset init. SHA-256
EXE: `96823853fe9f2c0a78e8fe39b33a3122e347f5cac58741474d12519a094dfa3d`;
SHA-256 SDL2.dll: `ce6025f4043fe059dea77439507942c5baa0caac4eba9aed06864704e8d30590`.
Хэши также лежат в `SHA256SUMS` рядом с файлами.

Пересборка Windows x64 прошла. `scripts/dtest.sh test` прошёл Go и native
регрессии, включая тесты async shader worker; `scripts/dtest.sh lint` сообщил
0 issues; `scripts/dtest.sh gl` прошёл SDL/OpenGL shared-context integration test.

Эта сборка не считается исправлением freeze: замеры фаз в логе с UHD 600 ещё
нужны, чтобы определить, что именно занимает `native_initialize_ms`. Релиз
остаётся заблокирован до проверки новой сборки на Windows 11 / Intel UHD 600:
закрыть окно крестиком (в том числе во время смены preset), выполнить несколько
переключений и сохранить новый лог. В логе ожидается `window close requested`
при нажатии крестика и новые поля `native_expressions_ms`,
`native_framebuffers_ms`, `native_warp_shader_ms`, `native_composite_shader_ms`.

## Лог и следующий кандидат 2026-09-22 12:00

Лог `glitchscope-20260922-120025.log` показывает, что три выбора заняли
1,70–2,16 с в `native_initialize_ms`. Expressions заняли 0,4–1,5 мс,
framebuffer 7–20 мс, а warp/composite shader phases — примерно 0,79–1,11 с
каждая. `shader_wait_us` составил 39–214 мс, `commit_us` — около 3–7 мс.
Основная синхронная работа — CPU-перевод MilkDrop HLSL до постановки GLSL в
shared-context очередь.

Следующая диагностическая сборка переносит чистое преобразование HLSL→GLSL на
тот же worker: в job копируются код шейдера, объявления sampler/texsize и enum
версии GLSL, без ссылок на preset или GL-backed descriptors. Ошибки пользовательского
warp по-прежнему отключают только custom warp; ошибка custom composite ставит в
очередь стандартный fallback. Добавлен тест с намеренно заблокированной CPU source
factory, который проверяет, что main-thread poll остаётся неблокирующим.

Эти исходные замеры предшествуют изменению; успешные mock/Go tests не подтверждают
результат на драйвере Intel. После полного Windows build нужен повтор на той же
машине: переключить несколько новых пресетов, проверить отзывчивость/закрытие и
передать лог. Релизный блокер остаётся активным до этой проверки.

## Windows candidate build 2026-09-22

`make -o builder dist-windows` успешно собрал полный Windows projectM и приложение
с этим patch. Кандидат лежит в `dist/windows-amd64/`; SHA-256
`glitchscope.exe`: `59884a39ed12a147231642418a32bcbc0bb0b051bb509f89c9b12a1a88f812be`,
`SDL2.dll`: `ce6025f4043fe059dea77439507942c5baa0caac4eba9aed06864704e8d30590`.
Файл `SHA256SUMS` находится рядом. Native worker mocks и Go tests прошли через
`scripts/dtest.sh test`. Этот EXE ещё не проверен на целевом компьютере и не
считается release build.

## Проверка на Windows 11 / UHD 600 2026-09-22 13:08

Лог `glitchscope-20260922-130830.log` снят с кандидата из предыдущего раздела
(SHA-256 `59884a39ed12a147231642418a32bcbc0bb0b051bb509f89c9b12a1a88f812be`). Он подтверждает
`shader compiler ready mode=shared-context`. Все десять переключений дошли до
commit; main-thread `gl_prepare_us` составил 3,5–39,7 мс, а между началом и
commit было от 2 до 143 кадров. Нажатие крестика завершилось `window close
requested` и `SoLoud shut down`, то есть оба исходных дефекта больше не
воспроизводятся на целевой машине.

Частота кадров действительно ниже цели 30 FPS на части пресетов. Лог содержит
кадры длительностью 100–370 мс; после выбора `Geometric/Stripes Circle/amandio
c - woofer.milk` (8 per-pixel equations) на протяжении десятков секунд render
занимал обычно 20–70 мс, а presentation — часто 60–300 мс. Проблема остаётся и
после завершения shader compile. Это может быть стоимость самого пресета,
драйвера/GPU или вывода кадра; для причинного вывода нужен контрольный замер
того же пресета на прежней сборке.

Строки adaptive resolution step в 13:10:22 и 13:10:25 не означают, что
контроллер повышает качество при перегрузке. Первое событие снижает размер до
960x540; второе отменяет эту пробу и восстанавливает 1280x720, потому что
снижение разрешения не улучшило измеренную стоимость/каденцию достаточно.
Поля utilization/cadence во второй строке относятся к окну пробы на 960x540.

## Финальный Windows x64 build 2026-09-22

После проверки исходников и удаления лишнего error wrapper кандидат повторно
собран командой `make -o builder dist-windows`. Актуальный комплект находится
в `dist/windows-amd64/`: SHA-256 `glitchscope.exe` —
`24f40c7e08fec95c80e39949a32fddb406202cc717d9a8e573755eb54098337b`, SHA-256
`SDL2.dll` — `ce6025f4043fe059dea77439507942c5baa0caac4eba9aed06864704e8d30590`.
`SHA256SUMS` лежит рядом. Сборка проверяет финальный исходный код; подтверждение
фризов на целевой машине относится к предыдущему EXE, а повторять performance
замер с этим бинарником нужно только для сравнения поведения после пересборки.
