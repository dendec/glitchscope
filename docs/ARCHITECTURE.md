# Архитектура GlitchScope

Этот документ описывает фактические границы модулей. Детали поведения UI
зафиксированы в [UI-PLAN.md](UI-PLAN.md), сборка и проверки — в [README.md](../README.md)
и [Makefile](../Makefile).

## Владельцы истины

| Понятие | Единственный владелец | Остальные проекции |
|---|---|---|
| Настройки | `internal/config` | JSON storage, Settings UI |
| Локальный индекс музыки | `internal/player` | NC navigation, playback selection |
| Навигация и focus | `internal/ui` | rendered lists and breadcrumbs |
| Playback queue | `internal/app` playback state | player commands, overlay snapshot |
| Удаление файлов | `internal/app/delete_service.go` | confirmation UI, rescan |
| Remote catalogs | `internal/modland`, `internal/modarchive` | provider navigation and downloads |

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

## Проверки перед изменением

```text
make test
make lint
make dist
```

`make test` и `make lint` запускаются внутри builder image, чтобы результаты
не зависели от локальных cgo-зависимостей. `make dist` дополнительно проверяет
полный amd64 packaging path.