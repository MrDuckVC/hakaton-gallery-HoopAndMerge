# A01: ручной аппаратный бенчмарк

Запускать из корня backend. Go-инструмент использует только существующие зависимости
проекта. Обычные тесты не обращаются к GPU/Ollama. Результаты оборудования и статус
приёмки — [MODEL_BENCHMARK.md](../../docs/MODEL_BENCHMARK.md).

Перед серией записать ОС, доступную память и питание без изменения настроек:

```sh
uname -a
cat /etc/os-release
nvidia-smi -q
free -h
powershell.exe -NoProfile -Command 'powercfg /getactivescheme; Add-Type -AssemblyName System.Windows.Forms; [System.Windows.Forms.SystemInformation]::PowerStatus | Format-List'
```

```sh
go build -o bin/modelbench ./tools/modelbench
mkdir -p data/model-benchmark
# Сначала подготовить model tag/окно через Ollama, см. DEVELOPMENT.md.
bin/modelbench -model gallery-guide-main:a01 -phase baseline -repeats 3 \
  -answers data/model-benchmark/baseline-private.jsonl \
  > data/model-benchmark/baseline.jsonl
```

`-url` по умолчанию `http://127.0.0.1:11434`. Запрос имеет ту же форму, что
production adapter: model/messages/stream; num_ctx/temperature берутся из
Modelfile. Reasoning модели не отключается скрытым benchmark-only параметром.

Серия содержит по три вопроса для двух профилей из examples/guides: объяснение,
продолжение с completed-парой и детерминированной памятью, затем чистая история
другого синтетического посетителя. Последний вопрос — синтетический русский вход
ровно 16000 кодовых точек с контрольным кодом в начале. Это проверка выбранного
текста, не гарантия вместимости любого Unicode-входа такой длины. Проверку
persisted memory и очереди выполняет отдельный SQL-сценарий ниже.

Каждая строка stdout — JSON с задержкой до первого **видимого** текста,
холодным режимом cold_runner, общим временем HTTP-запроса, load_duration и eval_count/eval_duration из Ollama,
длиной ответа, числом sampled observations и пиками общей памяти. tokens/s
Ollama может включать reasoning; TTFT включает ожидание reasoning. Холодная
загрузка входит в общее время. Ошибки, timeout, EOF без done, пустой ответ и
NUL/непарные суррогаты/чужая роль, output limit не считаются успешным завершением. Отмена/лимит закрывают HTTP body.

Телеметрия собирается до, примерно раз в секунду во время и после запроса:
nvidia-smi, /proc/meminfo и Windows Win32_OperatingSystem через powershell.exe.
Это **общая память системы**, включая браузер и другие приложения; RAM считается
как total − available, а не RSS процесса Ollama. Пики — нижняя оценка реального
пика при дискретном опросе. Затраты команд могут удлинять интервал опроса.
Недоступные датчики/метрики сохраняются как null. oom_reported означает явный
ответ об OOM; иные transport/HTTP failures требуют проверки локального журнала
Ollama и не доказывают отсутствие OOM.

Используйте отдельную серию `-cold -repeats 1` для выгрузки модели перед каждым
вопросом. Это холодный runner, не сброс файлового кеша ОС. Перед сменой модели
остановите предыдущую через `ollama stop <tag>`: иначе первый sampled peak новой
серии может включать ещё загруженную предыдущую модель. Для тёплой серии сначала
прогрейте выбранную модель и убедитесь через `ollama ps`, что окно и размещение
совпадают с ожидаемыми. Серии разных моделей не запускать одновременно.

Тексты не выводятся в stdout. `-answers` необязателен и создаёт новый приватный
файл 0600, отказываясь перезаписывать существующий. Использовать только
синтетические данные; файлы ответов в data/ не коммитить. Качество русского языка,
выполнение инструкций, сохранение контрольных кодов и отсутствие чужой памяти
проверяются человеком отдельно от скорости.

## Настоящая очередь backend

Требуется **отдельная** TEST_DATABASE_URL; тест создаёт и удаляет только свою schema.
Рабочую БД не использовать. Три вопроса от разных пользователей заранее сохраняются
через штатный Store, затем штатный worker выполняет их через настоящий Ollama.
Проверяются FIFO, отсутствие перекрытия выполнения, completed и сохранённая память.
Admission/HTTP/WS здесь не эмулируют готовый игровой показ.

```sh
A01_OLLAMA_MODEL=gallery-guide-main:a01 \
  go test ./internal/adapters/outbound/postgres \
  -run '^TestModelBenchmarkQueue$' -count=1 -v -timeout 10m
```

Повторить с резервом и с видимой браузерной сценой. Логи содержат только индекс
вопроса и времена ожидания/выполнения/TTFT. Без A01_OLLAMA_MODEL тест пропускается;
GPU-бенчмарк не включён в обычные go test и CI.

Для пиков системной памяти вокруг всей команды используйте уникальный каталог:

```sh
A01_OLLAMA_MODEL=gallery-guide-main:a01 \
  sh tools/modelbench/observe.sh "data/model-benchmark/queue-$(date -u +%Y%m%dT%H%M%SZ)" \
  go test ./internal/adapters/outbound/postgres \
  -run '^TestModelBenchmarkQueue$' -count=1 -v -timeout 10m
```

Перед переключением tag выгрузить предыдущую модель. observe.sh сохраняет
command.log, exit-status.txt, GPU CSV раз в секунду и Windows/WSL RAM samples;
сохраняет exit code команды и завершает собственные sampler-процессы через trap.
RAM включает также подготовку/процесс go test. Пустой/ошибочный датчик трактовать
как недоступный, а не нулевое потребление. TEST_DATABASE_URL должен быть заранее
задан только для отдельной тестовой БД.

## Сцена и офлайн

Перед `-phase scene` вручную открыть подготовленную сцену в браузере Windows,
дождаться зала и оставить вкладку видимой. Записать SHA frontend, браузер,
GPU renderer, состояние питания и фоновые приложения. Label phase — запись
оператора; инструмент не доказывает наличие браузера или отсутствие интернета.

Повторить серии после отключения интернета с сохранением LAN, затем после
выключения Wi-Fi сервера через localhost. `OLLAMA_NO_CLOUD=1` запрещает cloud
Ollama, но сам по себе не доказывает физическое отсутствие интернета.
Не отключать сеть пользователя автоматически.

Проверка именно модели в сетевой namespace WSL не требует отключать сеть Windows:

```sh
go build -o bin/modelbench ./tools/modelbench
go test -c -o bin/ollama-live.test ./internal/adapters/outbound/ollama
# Выгрузить текущую host-модель: в двух namespace нельзя оставлять два GPU runner.
ollama ps
ollama stop <текущий-tag-из-ps>
unshare --user --map-root-user --net sh tools/modelbench/offline.sh
```

Скрипт отказывается работать вне namespace с единственным интерфейсом lo,
сохраняет IPv4/IPv6 routes и неуспешную внешнюю проверку. Затем запускает собственный
Ollama, выполняет production-adapter fixtures и семь benchmark-запросов для каждого
из двух подготовленных aliases. Отказ fixture или любой не-completed результат
не создаёт PASS. Runner завершается через trap, Windows-сеть остаётся без изменений.
Результаты — новый приватный каталог в data/model-benchmark; путь можно задать
через A01_REPORT_DIR, существующий каталог не перезаписывается. Браузерная сцена
остаётся в host namespace; live SQL FIFO проверяется отдельно до этого запуска.
Эта проверка доказывает работу локальных весов без внешних маршрутов, но не
заменяет общий LAN/localhost игровой показ с физически выключенным Wi-Fi.
