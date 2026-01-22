# Архитектура AI-агента для управления Chrome

## Обзор

AI-агент для автономного управления видимым Chrome-браузером на Windows. Агент использует LLM (OpenAI) для планирования и принятия решений, Playwright для управления браузером, и структурированную память для контекста.

## Выбор технологий

### Playwright for Go
- **Почему**: Нативный контроль браузеров, поддержка persistent context (профили), стабильность, API для скриншотов
- **Альтернативы**: Selenium (медленнее, менее стабильно), Chrome DevTools Protocol напрямую (сложнее)
- **Использование**: `LaunchPersistentContext` с `headless=false` и `PROFILE_DIR`

### OpenAI Go SDK (go-openai)
- **Почему**: Официальная поддержка function/tool calling, Vision API для скриншотов, структурированные ответы
- **Использование**: GPT-4 Turbo для Planner/DOM-Analyst/Critic с tool calling

### Простой REPL
- **Почему**: Простая реализация через bufio.Scanner, достаточная для MVP
- **Использование**: Интерактивный REPL с командами (task, status, pause, resume, approve, deny, mode, help, quit)

### Zap (go.uber.org/zap)
- **Почему**: Высокая производительность, структурированное логирование, уровни
- **Использование**: JSON логи в `./runs/<timestamp>/run.log`, консольный вывод (human-readable)

### Godotenv + Viper
- **Почему**: Простая загрузка .env, валидация конфигов, типизированные структуры
- **Альтернативы**: envconfig (проще, но менее гибко)

### Cenkalti/backoff
- **Почему**: Экспоненциальный backoff для LLM и браузерных операций, ретраи с контекстом

## Архитектура компонентов

```
┌─────────────────────────────────────────────────────────────┐
│                        cmd/agent                            │
│                     (Wiring only)                           │
└────────────────────┬────────────────────────────────────────┘
                     │
                     ▼
┌─────────────────────────────────────────────────────────────┐
│                    internal/app                             │
│           (Dependency Injection, Init)                      │
└───┬──────────┬──────────┬──────────┬──────────┬────────────┘
    │          │          │          │          │
    ▼          ▼          ▼          ▼          ▼
┌────────┐ ┌────────┐ ┌────────┐ ┌────────┐ ┌────────┐
│ REPL   │ │ Agent  │ │ Browser│ │  LLM   │ │ Memory │
└───┬────┘ └───┬────┘ └───┬────┘ └───┬────┘ └───┬────┘
    │          │          │          │          │
    │    ┌─────┴─────┐    │          │          │
    │    │  Subagents│    │          │          │
    │    │  (Planner)│    │          │          │
    │    │  (Analyst)│    │          │          │
    │    │  (Critic) │    │          │          │
    │    └─────┬─────┘    │          │          │
    │          │          │          │          │
    │    ┌─────┴─────┐    │          │          │
    │    │  Vision   │    │          │          │
    │    │  Handler  │    │          │          │
    │    └─────┬─────┘    │          │          │
    │          │          │          │          │
    ▼          ▼          ▼          ▼          ▼
┌────────┐ ┌────────┐ ┌────────┐ ┌────────┐ ┌────────┐
│ Tools  │ │Security│ │Logging │ │ Config │ │DOMValid│
└────────┘ └────────┘ └────────┘ └────────┘ └────────┘
```

## Dataflow

### 1. Инициализация
```
User Input → REPL → App.Init() → Agent.Start()
```

### 2. Основной цикл
```
1. User: task "найди товар X на Ozon"
2. Agent → Planner: "что нужно сделать?"
3. Planner → JSON plan
4. Agent → executeNextStep:
   a. Observe (если нужно) → PageState
   b. Screenshot (для click/type)
   c. executeTool:
      - Vision handler (для click/type с screenshot) → Analyst.FindClickCoordinates → DOM validation → ClickAt/TypeAt
      - или обычный executor → Browser
   d. Progress check (после click/type/navigate/scroll)
   e. Memory: сохранить шаг
5. Agent → Critic (каждые N шагов): "всё ок?"
6. Repeat until goal or error
```

### 3. Memory Levels

```
EPHEMERAL (3 последних шага)
    ↓ (суммаризация)
WORKING SUMMARY (≤1500 символов)
    ↓ (извлечение фактов)
FACTS (JSON структура)
```

### 4. Tool Calling Flow

```
LLM → ToolCall JSON
    ↓ (валидация схемы)
Tool Registry → executeTool
    ↓ (для click/type с screenshot)
Vision Handler → Analyst.FindClickCoordinates
    ↓
DOM Validation → ClickAt/TypeAt или fallback
    ↓ (security check)
Security Layer → approve/deny
    ↓ (execution)
Browser → Result → Memory
```

## Subagents

### Planner
- **Роль**: Разбивает задачу на шаги
- **Вход**: Task description, current state, memory
- **Выход**: JSON plan с шагами
- **Вызывается**: При старте задачи, при блокировке

### DOM-Analyst
- **Роль**: Анализирует DOM и скриншоты, определяет координаты для клика
- **Вход**: Screenshot, target description, page context
- **Выход**: ClickCoordinates (x, y, element_text) или PageState (elements, overlays, text_digest)
- **Вызывается**: Для vision-анализа (FindClickCoordinates), для проверки прогресса (CheckProgress), для анализа страницы (AnalyzePage)

### Critic
- **Роль**: Проверяет прогресс, выявляет ошибки
- **Вход**: History, current state, plan
- **Выход**: JSON с оценкой и рекомендациями
- **Вызывается**: Каждые 5 шагов, при ошибках

## Security Layer

Опасные действия:
- Оплата (text содержит "оплата", "payment", "купить")
- Отправка формы (submit)
- Удаление (delete)
- Заказ (order, "оформить заказ")
- Отклик (apply)

Механизм: Intercept tool call → Check rules → REPL approve/deny → Continue/Abort

## Error Handling

1. **Ретраи**: Экспоненциальный backoff для LLM/network, до 5 попыток на шаг
2. **Recovery**: Planner обновляет план при ошибках → новый план
3. **Rollback**: При превышении лимита попыток → откат на предыдущий шаг
4. **Fallback**: Если vision не находит элемент → fallback на selector-based executor
5. **Progress Check**: После важных действий проверяется прогресс через vision-анализ
6. **Timeout**: Context с таймаутами на все операции

## Persistent Profile

- `PROFILE_DIR` (env): путь к user-data-dir
- Playwright `LaunchPersistentContext` с этим путём
- Cookies, localStorage сохраняются между запусками
- Пользователь логинится один раз вручную

## Структура памяти

### EPHEMERAL
```go
type EphemeralMemory struct {
    Steps []Step // последние 3 шага
}
```

### WORKING SUMMARY
```go
type WorkingSummary struct {
    Content string // ≤1500 символов
    UpdatedAt time.Time
}
```

### FACTS
```go
type Facts struct {
    URL string
    Goal string
    CompletedSteps []string
    Blockers []string
    Metadata map[string]interface{}
}
```

## Логирование и трассировка

- **Консоль**: Human-readable, уровни (debug/info/warn/error), zap
- **Файл**: `./runs/<timestamp>/run.log` (JSON, zap)
- **Trace**: `./runs/<timestamp>/trace.json` (tool calls, LLM requests)
- **Screenshots**: `./runs/<timestamp>/screens/step-XXX.png` (на каждом шаге для click/type)

## Конфигурация

`.env`:
```
OPENAI_API_KEY=sk-...
OPENAI_MODEL=gpt-4-turbo-preview
CHROME_PROFILE_DIR=C:\Users\...\ChromeProfile
BROWSER_TIMEOUT=30s
LLM_TIMEOUT=60s
LOG_LEVEL=info
MAX_RETRIES=3
```

## Тестирование

- **Unit**: memory, validation, security rules, plan parser
- **Integration**: mock browser, mock LLM → agent flow
- **E2E**: Не требуется (слишком сложно для MVP)

## Расширяемость

- Новые tools: добавить в `internal/tools` (ToolRegistry, Executor)
- Новые subagents: интерфейсы `PlannerInterface`, `AnalystInterface`, `CriticInterface` в `internal/subagents/interfaces.go`
- Новые security rules: `internal/security/rules.go`
- Новые LLM providers: интерфейс `LLMClient` в `internal/llm`
- Vision strategies: можно заменить `VisionActionHandler` на другую реализацию
- DOM validation: можно заменить `Validator` на другую реализацию (через интерфейс `Evaluator`)

## Модульная архитектура

После рефакторинга код разделён на чёткие модули:

- **`internal/domvalidation`**: DOM-валидация координат vision-анализа
- **`internal/navigation`**: Разрешение URL для навигации
- **`internal/vision`**: VisionActionHandler для обработки click/type через vision
- **`internal/subagents`**: Интерфейсы и реализации Planner, Analyst, Critic
- **`internal/agent`**: AgentOptions, AgentController интерфейс


