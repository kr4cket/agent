# AI Agent for Chrome Automation

Автономный AI-агент на Go для управления видимым Chrome-браузером на Windows. Агент использует LLM (OpenAI) для планирования и принятия решений, Playwright для управления браузером, и структурированную память для контекста.

## Особенности

- 🤖 Автономное управление видимым Chrome браузером
- 🧠 Использование LLM (OpenAI GPT-4) для планирования и принятия решений
- 👁️ Vision-анализ скриншотов для точного определения координат элементов
- 🔒 Security layer с подтверждением опасных действий
- 📝 Структурированная память (EPHEMERAL, WORKING SUMMARY, FACTS)
- 🎯 Subagents: Planner, DOM-Analyst, Critic (через интерфейсы)
- 📸 Скриншоты на каждом шаге для vision-анализа
- 📊 Логирование и трассировка всех действий
- 🔄 REPL для интерактивного управления
- 🏗️ Модульная архитектура с чётким разделением ответственности

## Требования

- Go 1.22 или выше
- Windows 10/11
- Google Chrome установлен
- OpenAI API Key

## Установка

1. Клонируйте репозиторий:
```bash
git clone <repository-url>
cd testTask
```

2. Установите зависимости:
```bash
go mod download
```

3. Установите Playwright browsers:
```bash
go run github.com/playwright-community/playwright-go/cmd/playwright@latest install chromium
```

4. Создайте файл `.env` на основе `configs/env.example`:
```bash
copy configs\env.example .env
```

5. Отредактируйте `.env` и укажите:
   - `OPENAI_API_KEY` - ваш OpenAI API ключ
   - `CHROME_PROFILE_DIR` - путь к профилю Chrome (см. инструкцию ниже)

## Настройка Chrome Profile

**ВАЖНО**: Перед запуском нужно правильно настроить путь к профилю Chrome.

### Способ 1: Использовать существующий профиль Chrome (рекомендуется для Google аккаунтов)

1. Найдите путь к вашему профилю Chrome:
   - Откройте Chrome
   - Введите в адресной строке: `chrome://version/`
   - Найдите строку "Profile Path"
   - Обычно это: `C:\Users\<ВашеИмя>\AppData\Local\Google\Chrome\User Data\Default`
   - Если используете несколько профилей, может быть `Profile 1`, `Profile 2` и т.д.

2. Укажите этот путь в `.env`:
   ```
   CHROME_PROFILE_DIR=C:\Users\Daniil Koreshkov\AppData\Local\Google\Chrome\User Data\Default
   ```

3. **ВАЖНО**: 
   - Закройте Chrome перед запуском агента, иначе будет ошибка доступа к профилю
   - Убедитесь, что вы уже залогинены в нужные аккаунты (Google, Ozon, Yandex Market и т.д.) в этом профиле
   - Агент автоматически обходит детекцию автоматизации Google, используя специальные флаги Chrome

### Способ 2: Создать отдельный профиль для агента

1. Создайте новую директорию для профиля (например, `C:\ChromeAgentProfile`)

2. Укажите этот путь в `.env`:
   ```
   CHROME_PROFILE_DIR=C:\ChromeAgentProfile
   ```

3. При первом запуске Chrome создаст новый профиль в этой директории
4. Вам нужно будет вручную залогиниться в нужные сервисы при первом использовании

### Работа с Google аккаунтами

Агент автоматически обходит детекцию автоматизации Google с помощью:
- Флагов Chrome: `--disable-blink-features=AutomationControlled`, `--exclude-switches=enable-automation`
- JavaScript-скрипта, скрывающего `navigator.webdriver`

Это позволяет использовать уже авторизованный профиль Chrome без блокировок Google. Если вы видите ошибку "Не удалось войти в аккаунт", убедитесь, что:
1. Используете правильный путь к профилю (где вы уже залогинены)
2. Chrome полностью закрыт перед запуском агента
3. Профиль не заблокирован другим процессом

## Запуск

### Через Makefile (рекомендуется):

```bash
make run
```

### Напрямую:

```bash
go run cmd/agent/main.go
```

## Использование REPL

После запуска доступны следующие команды:

- `task <description>` - Запустить новую задачу (например: `task найти товар iPhone на Ozon и добавить в корзину`)
- `status` - Показать статус агента
- `pause` - Приостановить выполнение
- `resume` - Возобновить выполнение
- `approve` - Одобрить опасное действие
- `deny` - Отклонить опасное действие
- `mode <live|dryrun>` - Установить режим (live - реальное выполнение, dryrun - только логирование)
- `help` - Показать справку
- `quit` / `exit` - Выход

### Пример сессии:

```
agent> task найти товар iPhone на Ozon и открыть корзину
Started task: найти товар iPhone на Ozon и открыть корзину
agent> status
Status:
  state: running
  task: найти товар iPhone на Ozon и открыть корзину
  step_count: 5
  dry_run: false
  plan_progress: 5/8
  blocked: false
agent> mode dryrun
Mode set to: dryrun
agent> quit
Goodbye!
```

## Структура проекта

```
testTask/
├── cmd/
│   └── agent/           # Точка входа CLI
├── internal/
│   ├── agent/          # Оркестратор агента (AgentController, AgentOptions)
│   ├── app/            # Инициализация и DI
│   ├── browser/        # Playwright adapter
│   ├── config/         # Конфигурация
│   ├── domvalidation/  # DOM-валидация координат vision-анализа
│   ├── llm/            # OpenAI adapter
│   ├── logging/        # Логирование
│   ├── memory/         # Управление памятью
│   ├── navigation/     # URL resolution для navigate
│   ├── repl/           # REPL интерфейс
│   ├── security/       # Security layer
│   ├── subagents/      # Planner, DOM-Analyst, Critic (интерфейсы + реализации)
│   ├── tools/          # Tool registry и executors
│   └── vision/         # VisionActionHandler для vision-анализа click/type
├── configs/            # Примеры конфигов
├── docs/               # Документация
├── scripts/            # Утилиты
├── runs/               # Логи и скриншоты (создаётся автоматически)
├── Makefile
├── .golangci.yml
└── README.md
```

## Логи и трассировка

Все логи сохраняются в `./runs/<timestamp>/`:

- `run.log` - JSON логи всех операций
- `trace.json` - Трассировка LLM вызовов и tool calls
- `screens/` - Скриншоты на каждом шаге (`step-XXX.png`)

## Демо-сценарий

1. Запустите агента:
```bash
make run
```

2. В REPL введите задачу:
```
agent> task найти товар iPhone на Ozon, добавить в корзину и открыть корзину
```

3. Агент автоматически:
   - Откроет Ozon
   - Найдёт товар iPhone
   - Добавит в корзину
   - Откроет корзину
   - Сделает скриншоты каждого шага

4. Если потребуется подтверждение опасного действия (например, оформление заказа), агент запросит approval через REPL.

## Запись видео (OBS)

Для записи демо-видео:

1. Откройте OBS Studio
2. Добавьте источник "Window Capture"
3. Выберите окно Chrome, которое управляется агентом
4. Начните запись
5. Запустите агента и выполните задачу
6. Остановите запись

## Разработка

### Сборка:

```bash
make build
```

### Тесты:

```bash
make test
```

### Линтер:

```bash
make lint
```

### Форматирование:

```bash
make fmt
```

## Безопасность

- Агент автоматически запрашивает подтверждение перед опасными действиями:
  - Оплата
  - Оформление заказа
  - Отправка форм
  - Удаление
  - Отклик на вакансию

- Используйте `mode dryrun` для тестирования без реальных действий.

## Архитектура

Подробное описание архитектуры и рефакторинга см. в:
- [docs/architecture.md](docs/architecture.md) - общая архитектура
- [docs/architecture-refactoring.md](docs/architecture-refactoring.md) - детали рефакторинга и текущее состояние

### Ключевые компоненты

- **Agent** - оркестратор, управляет выполнением плана
- **Subagents** - Planner (планирование), Analyst (vision-анализ), Critic (критика плана)
- **VisionActionHandler** - обработка click/type через vision-анализ скриншотов
- **DOMValidation** - валидация координат через DOM-анализ
- **Navigation** - разрешение URL для навигации
- **Browser** - Playwright adapter для управления Chrome
- **Memory** - структурированная память (Ephemeral, WorkingSummary, Facts)

## Ограничения

- Работает только на Windows (можно адаптировать для Linux/Mac)
- Требует установленный Chrome
- Требует OpenAI API ключ
- Видимый браузер (headless=false) - жёсткое требование
- Не автоматизирует логин/капчу (пользователь должен быть уже залогинен)

## Лицензия

MIT

## Автор

Senior Go Engineer + AI Agent Architect
