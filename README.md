# worker-ghb-playwright

Воркер для автоматического уведомления и регистрации на объекты застройщика **GHB** через браузерную автоматизацию (
Playwright / Chromium headless).

**Застройщик:** GHB (зашит в бинарь)
**Лицензия:** MIT

---

## Установка

### Linux / macOS

```bash
curl -L https://github.com/stroi-homes/worker-ghb-playwright/releases/latest/download/worker-ghb-playwright-linux-amd64 \
  -o worker-ghb-playwright
chmod +x worker-ghb-playwright
./worker-ghb-playwright --config config.yaml
# При первом запуске автоматически скачивается Chromium (~150 MB)
```

### Windows

Скачать `worker-ghb-playwright-windows-amd64.exe` со страницы [Releases](../../releases) и запустить. Chromium скачается
автоматически при первом запуске в `%APPDATA%\worker-ghb-playwright\chromium\`.

---

## Chromium

При первом запуске бинарь автоматически скачивает нужную версию Chromium (~150 MB):

```
Проверка Chromium в кэше: ~/.worker-ghb-playwright
Загрузка Chromium... [====================] 150 MB
Chromium готов к использованию
```

Последующие запуски используют кэш без загрузки.

**Принудительное обновление Chromium:**

```bash
./worker-ghb-playwright --update-browser
```

---

## Настройка

### Инициализация

```bash
./worker-ghb-playwright init --config config.yaml
```

Конфиг идентичен `worker-ghb-http` (файлы взаимозаменяемы). Секция `personal_data` шифруется AES-256-GCM.

### Структура конфига

| Параметр                        | Описание                                           |
|---------------------------------|----------------------------------------------------|
| `service.base_url`              | URL сервера (обычно `https://stroi.homes`)         |
| `service.use_sse`               | Использовать Server-Sent Events вместо polling     |
| `service.poll_interval_seconds` | Интервал опроса в секундах (если `use_sse: false`) |
| `telegram.enabled`              | Включить уведомления в Telegram                    |
| `telegram.bot_token`            | Токен Telegram-бота (получить у @BotFather)        |
| `telegram.chat_id`              | ID чата для уведомлений                            |
| `personal_data.full_name`       | ФИО (шифруется)                                    |
| `personal_data.phone`           | Номер телефона (шифруется)                         |
| `watch_list`                    | Список объектов для отслеживания                   |

### Как получить chat_id

1. Создайте бота у [@BotFather](https://t.me/BotFather) и получите токен
2. Добавьте бота в чат (группу или канал)
3. Отправьте любое сообщение в чат
4. Перейдите по ссылке: `https://api.telegram.org/bot<TOKEN>/getUpdates`
    - Замените `<TOKEN>` на токен вашего бота
5. В ответе найдите поле `chat.id` — это и есть `chat_id` (отрицательное число для групп)

**Для личных сообщений:**

1. Начните чат с вашим ботом (@username бота)
2. Откройте `https://api.telegram.org/bot<TOKEN>/getUpdates`
3. Найдите поле `chat.id` в секции `from` — это ваш личный `chat_id`

Пример ответа:

```json
{
  "ok": true,
  "result": [
    {
      "update_id": 123456789,
      "message": {
        "chat": {
          "id": -987654321,
          "title": "My Group"
        }
      }
    }
```

---

## Запуск

```bash
./worker-ghb-playwright --config config.yaml
# запрашивается пароль для расшифровки personal_data

# Или с паролем через env:
WORKER_PASSWORD=мой_пароль ./worker-ghb-playwright --config config.yaml
```

---

## Скриншоты при ошибках

При ошибке во время авторегистрации сохраняется скриншот:

- Linux/macOS: `~/.worker-ghb-playwright/screenshots/`
- Windows: `%APPDATA%\worker-ghb-playwright\screenshots\`

---

## Требования к системе

- Нет зависимостей ОС — единый статический бинарь
- Chromium: ~150 MB (однократная загрузка)
- RAM в рантайме: ~200–400 MB

---

## Сборка из исходников

```bash
go build -o worker-ghb-playwright .
make dist  # все платформы
```

Требования: Go 1.22+
