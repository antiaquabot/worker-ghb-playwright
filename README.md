# worker-ghb-playwright

Воркер для автоматического уведомления и регистрации на объекты застройщика **GHB** через браузерную автоматизацию (Playwright / Chromium headless).

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

Скачать `worker-ghb-playwright-windows-amd64.exe` со страницы [Releases](../../releases) и запустить. Chromium скачается автоматически при первом запуске в `%APPDATA%\worker-ghb-playwright\chromium\`.

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

```bash
./worker-ghb-playwright init --config config.yaml
```

Конфиг идентичен `worker-ghb-http` (файлы взаимозаменяемы). Секция `personal_data` шифруется AES-256-GCM.

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
