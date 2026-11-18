# Habitat API

![CI](https://github.com/trefas/habitat/actions/workflows/ci.yml/badge.svg)

REST API сервера **Habitat** на Go. Сервис работает с PostgreSQL, поддерживает
регистрацию и аутентификацию по JWT, refresh-токены с ротацией и обнаружением
повторного использования, смену пароля и административное управление
пользователями.

## Возможности

- Регистрация, вход, обновление токенов и выход (`register`, `login`, `refresh`, `logout`).
- Access-токены JWT (HS256) и refresh-токены с хранением только SHA-256-хеша.
- Ротация refresh-токенов в транзакции + reuse detection: повторное использование
  отозванного токена отзывает всё семейство сессий.
- Защита от timing-атак при неверном email и пароле.
- Смена пароля с отзывом всех активных сессий.
- Смена собственного отображаемого имени и пароля (`PATCH /me`, `PATCH /me/password`).
- Административный CRUD пользователей: список с фильтрами и пагинацией,
  просмотр, изменение роли/статуса/имени, удаление.
- Защита от потери доступа: нельзя удалить или понизить последнего активного
  администратора, нельзя изменить роль/статус или удалить самого себя.
- Миграции БД применяются автоматически при старте.
- Health/readiness-пробы, graceful shutdown, конфигурация через переменные
  окружения и файловые секреты.
- CI (gofmt, vet, build, race-тесты с PostgreSQL) и CD (публикация образа в GHCR).

## Стек

- Go 1.27
- PostgreSQL 16
- [pgx/v5](https://github.com/jackc/pgx) — драйвер и пул соединений
- [golang-migrate](https://github.com/golang-migrate/migrate) — миграции
- [golang-jwt/jwt/v5](https://github.com/golang-jwt/jwt) — JWT
- bcrypt (`golang.org/x/crypto`) — хеширование паролей

## Структура проекта

```
cmd/api/                        точка входа (composition root), роутинг, graceful shutdown
internal/config/                конфигурация из env и файловых секретов
internal/domain/                доменные сущности, константы и ошибки
internal/auth/                  bcrypt, JWT, refresh-токены, бизнес-логика аутентификации
internal/users/                 бизнес-логика административного управления пользователями
internal/httpapi/               HTTP-хендлеры, middleware, единый формат ответов
internal/storage/postgres/      реализация хранилища на pgx
migrations/                     SQL-миграции (up/down)
```

Зависимости направлены внутрь: транспорт (`httpapi`) → сервисы (`auth`,
`users`) → интерфейс хранилища ← `storage/postgres`. Доменный слой не зависит
от инфраструктуры.

## Быстрый старт (Docker Compose)

Секреты не хранятся в репозитории (каталог `secrets/` в `.gitignore`), поэтому
перед первым запуском их нужно создать:

```bash
mkdir -p secrets
printf '%s' 'change-me-strong-db-password' > secrets/pg_password.txt
openssl rand -base64 48 | tr -d '\n' > secrets/jwt_secret.txt

docker compose up --build -d
```

Сервис будет доступен на `http://localhost:8080`:

```bash
curl http://localhost:8080/healthz   # ok
curl http://localhost:8080/readyz    # ready (проверяет БД)
```

Остановка:

```bash
docker compose down          # без удаления данных
docker compose down -v       # вместе с томом PostgreSQL
```

> JWT-секрет должен быть не короче 32 символов. Приложение не стартует, если
> секрет или пароль БД не заданы.

## Локальный запуск без Docker

Нужен запущенный PostgreSQL и созданная база:

```bash
createdb habitat

export DB_HOST=localhost DB_PORT=5432 DB_USER=habitat DB_NAME=habitat
export DB_PASSWORD='your-password'
export JWT_SECRET="$(openssl rand -base64 48 | tr -d '\n')"
export MIGRATIONS_PATH="file://$(pwd)/migrations"

go run ./cmd/api
```

Миграции применяются автоматически при старте.

## Конфигурация

| Переменная | По умолчанию | Описание |
|---|---|---|
| `DB_HOST` | `localhost` | Хост PostgreSQL |
| `DB_PORT` | `5432` | Порт PostgreSQL |
| `DB_USER` | `habitat` | Пользователь БД |
| `DB_NAME` | `habitat` | Имя базы |
| `DB_PASSWORD` | — | Пароль БД (обязателен, если не задан `DB_PASSWORD_FILE`) |
| `DB_PASSWORD_FILE` | — | Путь к файлу с паролем БД; имеет приоритет над `DB_PASSWORD` |
| `JWT_SECRET` | — | Секрет подписи JWT, минимум 32 символа |
| `JWT_SECRET_FILE` | — | Путь к файлу с JWT-секретом; имеет приоритет над `JWT_SECRET` |
| `MIGRATIONS_PATH` | `file:///app/migrations` | Источник миграций (для локального запуска переопределите) |
| `HTTP_ADDR` | `:8080` | Адрес HTTP-сервера |
| `ACCESS_TOKEN_TTL` | `15m` | Время жизни access-токена |
| `REFRESH_TOKEN_TTL` | `720h` | Время жизни refresh-токена (30 дней) |
| `BCRYPT_COST` | `12` | Стоимость bcrypt (допустимо 10–14) |

Пароль БД и JWT-секрет читаются из файлов и очищаются от пробельных символов по
краям — это удобно для Docker secrets и Kubernetes.

## API

Базовый префикс: `/api/v1`. Формат данных — JSON (`Content-Type: application/json`).

### Служебные

| Метод | Путь | Описание |
|---|---|---|
| `GET` | `/healthz` | Liveness-проба |
| `GET` | `/readyz` | Readiness-проба (пинг БД) |
| `GET` | `/` | Приветствие |

### Аутентификация

| Метод | Путь | Доступ | Описание |
|---|---|---|---|
| `POST` | `/api/v1/auth/register` | публично | Регистрация |
| `POST` | `/api/v1/auth/login` | публично | Вход |
| `POST` | `/api/v1/auth/refresh` | публично | Обновление пары токенов |
| `POST` | `/api/v1/auth/logout` | публично | Отзыв семейства refresh-токенов |
| `GET` | `/api/v1/me` | Bearer | Текущий пользователь |
| `PATCH` | `/api/v1/me` | Bearer | Смена собственного `display_name` |
| `PATCH` | `/api/v1/me/password` | Bearer | Смена пароля |

Регистрация:

```bash
curl -X POST http://localhost:8080/api/v1/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"email":"user@example.com","password":"super-secret-1","display_name":"User"}'
```

Вход:

```bash
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"user@example.com","password":"super-secret-1"}'
```

Ответ содержит `access_token`, `refresh_token`, `token_type` (`Bearer`),
`expires_in` и данные пользователя.

Обновление токенов (старый refresh-токен становится недействительным):

```bash
curl -X POST http://localhost:8080/api/v1/auth/refresh \
  -H 'Content-Type: application/json' \
  -d '{"refresh_token":"<refresh>"}'
```

Выход (204 No Content):

```bash
curl -X POST http://localhost:8080/api/v1/auth/logout \
  -H 'Content-Type: application/json' \
  -d '{"refresh_token":"<refresh>"}'
```

Профиль и смена пароля:

```bash
curl http://localhost:8080/api/v1/me \
  -H "Authorization: Bearer $ACCESS_TOKEN"

curl -X PATCH http://localhost:8080/api/v1/me \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"display_name":"Новое имя"}'

curl -X PATCH http://localhost:8080/api/v1/me/password \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"current_password":"super-secret-1","new_password":"even-better-secret-2"}'
```

`PATCH /api/v1/me` принимает только `display_name` (1–100 символов после
обрезки пробелов). Email, роль и статус пользователь изменить не может —
неизвестные поля отклоняются с `400 invalid_json`; для этого используйте
административные маршруты.

После смены пароля все refresh-токены пользователя отзываются — потребуется
войти заново.

### Администрирование пользователей

Все маршруты требуют `Authorization: Bearer <access-token>` с ролью `admin`.

| Метод | Путь | Описание |
|---|---|---|
| `GET` | `/api/v1/admin/users` | Список пользователей |
| `GET` | `/api/v1/admin/users/{id}` | Пользователь по ID |
| `PATCH` | `/api/v1/admin/users/{id}` | Изменить `display_name`, `role`, `status` |
| `DELETE` | `/api/v1/admin/users/{id}` | Удалить пользователя |

Параметры списка:

| Параметр | Описание |
|---|---|
| `search` | Поиск по email и `display_name` (регистронезависимый, до 100 символов) |
| `role` | `user` или `admin` |
| `status` | `pending`, `active` или `blocked` |
| `limit` | Размер страницы, 1–100 (по умолчанию 20) |
| `offset` | Смещение, ≥ 0 |

```bash
curl 'http://localhost:8080/api/v1/admin/users?role=user&status=active&search=example&limit=20&offset=0' \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

```json
{
  "items": [
    {
      "id": "…",
      "email": "user@example.com",
      "display_name": "User",
      "role": "user",
      "status": "active",
      "created_at": "2026-11-18T10:00:00Z",
      "updated_at": "2026-11-18T10:00:00Z",
      "last_login_at": "2026-11-18T10:05:00Z"
    }
  ],
  "total": 1,
  "limit": 20,
  "offset": 0
}
```

Изменение пользователя (все поля необязательны, но хотя бы одно должно быть передано):

```bash
curl -X PATCH http://localhost:8080/api/v1/admin/users/<id> \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"display_name":"New Name","role":"admin","status":"blocked"}'
```

При блокировке (`status != active`) или смене роли все refresh-токены
пользователя отзываются. Администратор не может изменить собственную роль или
статус, а также удалить себя. Попытка понизить/заблокировать/удалить последнего
активного администратора возвращает `409 last_admin`.

Удаление:

```bash
curl -X DELETE http://localhost:8080/api/v1/admin/users/<id> \
  -H "Authorization: Bearer $ADMIN_TOKEN"   # 204 No Content
```

### Как получить первого администратора

Регистрация всегда создаёт пользователя с ролью `user`. Первого администратора
нужно назначить вручную через SQL:

```bash
docker compose exec db psql -U habitat -d habitat \
  -c "UPDATE users SET role = 'admin' WHERE email = 'admin@example.com';"
```

После этого нужно войти заново, чтобы получить access-токен с ролью `admin`
(старый токен действует до истечения TTL).

### Ошибки

Единый формат:

```json
{
  "error": "validation_error",
  "message": "подробности"
}
```

| HTTP | `error` | Когда возникает |
|---|---|---|
| 400 | `invalid_json` | Некорректный или пустой JSON |
| 400 | `validation_error` | Неверные поля, параметры или идентификатор |
| 401 | `invalid_token` | Нет или истёк Bearer-токен |
| 401 | `invalid_credentials` | Неверные email/пароль или refresh-токен |
| 403 | `user_blocked` | Аккаунт заблокирован или не активирован |
| 403 | `forbidden` | Недостаточно прав (не admin) |
| 404 | `not_found` | Пользователь/ресурс не найден |
| 409 | `email_taken` | Email уже зарегистрирован |
| 409 | `last_admin` | Попытка убрать последнего активного администратора |
| 500 | `internal_error` | Внутренняя ошибка |

## Безопасность

- Пароли хранятся только в виде bcrypt-хешей (cost 12 по умолчанию).
- Access-токен подписывается HS256; при разборе жёстко проверяются алгоритм,
  издатель и обязательный `exp`.
- Refresh-токен — 256 бит CSPRNG; в БД хранится только его SHA-256-хеш.
- Ротация refresh-токенов атомарна (`SELECT … FOR UPDATE`), повторное
  использование отозванного токена трактуется как компрометация и отзывает всё
  семейство.
- При неизвестном email выполняется фиктивное bcrypt-сравнение для выравнивания
  времени ответа.
- Секреты не попадают в образ и репозиторий; поддерживается чтение из файлов.

Ограничения текущей версии: нет rate limiting и CAPTCHA на login/register, нет
подтверждения email и восстановления пароля, нет списка активных устройств.
Access-токен остаётся валидным до истечения TTL даже после блокировки
пользователя (refresh при этом уже не выдаётся).

## Тесты

```bash
make check            # gofmt + go vet + go test -race ./...
```

Интеграционные тесты хранилища включаются при заданном `TEST_DSN`:

```bash
TEST_DSN='postgres://habitat:password@localhost:5432/habitat?sslmode=disable' \
  go test -race -count=1 ./...
```

> Интеграционные тесты применяют миграции и делают `TRUNCATE users CASCADE`.
> Используйте отдельную тестовую базу, а не рабочую.

## CI/CD

- `.github/workflows/ci.yml` — gofmt, проверка актуальности `go.mod/go.sum`,
  `go vet`, сборка и race-тесты с сервисом PostgreSQL.
- `.github/workflows/cd.yml` — сборка Docker-образа и публикация в
  `ghcr.io/trefas/habitat` при push в `main`/`master` и при создании тега `v*`.

Релиз:

```bash
git tag v1.1.0
git push origin v1.1.0
```

## Лицензия

См. [LICENSE](LICENSE).
