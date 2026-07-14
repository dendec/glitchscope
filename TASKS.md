# MDPP — Задачи

## 1 — Скелет (текущая)
go.mod, cmd/mdpp/main.go (SDL2 окно 640x480, OpenGL контекст, ESC выход),
Makefile, Dockerfile.arm64.

## 2 — Аудио
SoLoud вендор + cgo мост, internal/soloud/, internal/player/player.go
(минимальный: открыть MP3 рядом с бинарём).

## 3 — Визуализация
projectM вендор + cgo мост, internal/projectm/, + один .milk пресет,
фейковый FFT для теста.

## 4 — Интеграция
FFT из SoLoud → projectM → рендер на экран. Полный цикл: MP3 → звук + MilkDrop.
