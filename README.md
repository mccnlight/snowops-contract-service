# Contract Service

Contract-service отвечает за управление контрактами (договорами) на уборку снега. Он использует PostgreSQL и предоставляет REST API, защищённый JWT-токенами, выпускаемыми `auth-service`.

## Highlights

- Derived fields (`contract_ui_status`, `contract_result`, `payable_amount`, `budget_exceeded`, `volume_progress`) are calculated for every contract response based on the accumulated `contract_usage`.
- Contracts are immutable after they are created to mirror the PDF requirements; no PATCH or DELETE endpoints exist in this service.
- The global `ticket` table now has a mandatory `contract_id` foreign key. Binding happens exactly once via `PUT /tickets/:ticket_id/contract`.
- Each trip volume is reported through `POST /trips/usage`, which updates both `contract_usage` and the immutable `trip_usage_log`.

## Возможности

- Управление контрактами:
  - `KGU_ZKH_ADMIN` — единственный, кто создаёт контракты (без редактирования/удаления после сохранения).
  - `AKIMAT_ADMIN` — полный read-only, может фиксировать usage для аудита.
  - `CONTRACTOR_ADMIN` — read-only только по своим контрактам.
  - `TOO_ADMIN`, `DRIVER` — нет доступа.
- Отслеживание использования через `contract_usage`:
  - Накопленный объём вывезенного снега (`total_volume_m3`).
  - Накопленная стоимость (`total_cost`) и расчёт `payable_amount = min(total_cost, budget_total)`.
- Фильтры списка контрактов по подрядчику, типу работ, статусу (`PLANNED|ACTIVE|EXPIRED|ARCHIVED`) и диапазонам `start_at`/`end_at`.
- Подробные данные по каждому контракту:
  - `/contracts/:id/tickets` — список тикетов с прогрессом и счётчиками.
  - `/contracts/:id/trips` — рейсы, которые легли в usage.

## Структура данных

### Contract (Контракт)
- `contractor_id` — подрядчик (CONTRACTOR)
- `created_by_org` — кто создал (Акимат/ТОО)
- `work_type` — тип работ: `road`, `sidewalk`, `yard`
- `price_per_m3` — цена за кубометр
- `budget_total` — максимальная сумма по договору
- `minimal_volume_m3` — минимальный обязательный объём вывоза
- `start_at`, `end_at` — период действия контракта

### Contract Usage (Использование контракта)
- `total_volume_m3` — накопленный объём
- `total_cost` — накопленная стоимость

## Запуск локально

```bash
# поднять Postgres
cd deploy
docker compose up -d

# запустить сервис
cd ..
APP_ENV=development \
DB_DSN="postgres://postgres:postgres@localhost:5434/contract_db?sslmode=disable" \
JWT_ACCESS_SECRET="secret-key" \
HTTP_HOST=0.0.0.0 \
HTTP_PORT=7082 \
go run ./cmd/contract-service
```

### Переменные окружения

| Переменная             | Описание                                      | Значение по умолчанию              |
|------------------------|-----------------------------------------------|------------------------------------|
| `APP_ENV`              | окружение (`development`, `production`)       | `development`                      |
| `HTTP_HOST`            | хост для HTTP сервера                         | `0.0.0.0`                          |
| `HTTP_PORT`            | порт для HTTP сервера                         | `7082`                             |
| `DB_DSN`               | строка подключения к PostgreSQL               | обязательная                       |
| `DB_MAX_OPEN_CONNS`    | максимальное количество открытых соединений   | `25`                               |
| `DB_MAX_IDLE_CONNS`    | максимальное количество простаивающих соединений | `10`                            |
| `DB_CONN_MAX_LIFETIME` | максимальное время жизни соединения           | `1h`                               |
| `JWT_ACCESS_SECRET`    | секретный ключ для проверки JWT токенов       | обязательная                       |

## API Endpoints

Все эндпоинты требуют JWT аутентификацию через заголовок `Authorization: Bearer <token>`.

> Таблица `tickets` и колонка `contract_id` управляются сервисом `snowops-tickets`. Contract-service использует уже готовую схему и не выполняет миграций по тикетам; убедитесь, что миграции ticket-service выполняются первыми.

### Health Check

#### GET /healthz
Проверка работоспособности сервера (без аутентификации)

**Ответ:**
```json
{
  "status": "ok"
}
```

### Контракты

#### GET /contracts
Получить список контрактов

- Параметры фильтрации:
  - `contractor_id` — UUID подрядчика.
  - `work_type` — `road`, `sidewalk`, `yard`.
  - `status` — `PLANNED`, `ACTIVE`, `EXPIRED`, `ARCHIVED`.
  - `only_active` — true/false (игнорируется, если задан `status`).
  - `start_from`, `start_to`, `end_from`, `end_to` — границы периода (RFC3339).

**Ответ:**
```json
{
  "data": [
    {
      "id": "uuid",
      "contractor_id": "uuid",
      "created_by_org_id": "uuid",
      "name": "Контракт на уборку дорог",
      "work_type": "road",
      "price_per_m3": 1500.00,
      "budget_total": 1000000.00,
      "minimal_volume_m3": 500.00,
      "start_at": "2024-01-01T00:00:00Z",
      "end_at": "2024-12-31T23:59:59Z",
      "is_active": true,
      "created_at": "2024-01-01T00:00:00Z",
      "usage": {
        "total_volume_m3": 250.50,
        "total_cost": 375750.00
      }
    }
  ]
}
```

#### POST /contracts
Создать новый контракт (только `KGU_ZKH_ADMIN`)

**Тело запроса:**
```json
{
  "contractor_id": "uuid",
  "name": "Контракт на уборку дорог",
  "work_type": "road",
  "price_per_m3": 1500.00,
  "budget_total": 1000000.00,
  "minimal_volume_m3": 500.00,
  "start_at": "2024-01-01T00:00:00Z",
  "end_at": "2024-12-31T23:59:59Z",
  "is_active": true
}
```

**Ответ:** 201 Created с созданным контрактом

#### GET /contracts/:id
Получить контракт по ID (read-only карточка со всеми вычисляемыми полями)

#### GET /contracts/:id/tickets
Таблица тикетов, привязанных к контракту.

**Ответ:** 200 OK
```json
{
  "data": [
    {
      "id": "uuid",
      "cleaning_area_id": "uuid",
      "cleaning_area_name": "Участок №1",
      "planned_start_at": "2024-01-02T00:00:00Z",
      "planned_end_at": "2024-01-05T12:00:00Z",
      "status": "PLANNED",
      "trip_count": 3,
      "total_volume_m3": 120.5,
      "active_assignments": 2
    }
  ]
}
```

#### GET /contracts/:id/trips
Рейсы (`trips`) по тикетам контракта, отсортированные по времени въезда.

**Ответ:** 200 OK
```json
{
  "data": [
    {
      "id": "uuid",
      "ticket_id": "uuid",
      "driver_id": "uuid",
      "vehicle_id": "uuid",
      "entry_at": "2024-01-03T01:23:00Z",
      "exit_at": "2024-01-03T02:10:00Z",
      "status": "OK",
      "detected_volume_entry": 42.3,
      "detected_volume_exit": 2.1
    }
  ]
}
```

### PUT /tickets/:ticket_id/contract
Сопоставить тикет с контрактом (единожды).

**Доступ:** `KGU_ZKH_ADMIN`

```json
{
  "contract_id": "uuid"
}
```

**Ответ:** 200 OK

### POST /trips/usage
Зафиксировать рейс и обновить `contract_usage`.

**Доступ:** `KGU_ZKH_ADMIN`, `AKIMAT_ADMIN`

```json
{
  "trip_id": "uuid",
  "ticket_id": "uuid",
  "detected_volume_m3": 25.5
}
```

**Ответ:** 201 Created (409 при повторном trip_id) после успешного пересчёта usage.

## Права доступа

| Роль              | Возможности                                                        |
|-------------------|--------------------------------------------------------------------|
| `KGU_ZKH_ADMIN`   | Создаёт контракты, читает все, линкует тикеты, пишет usage         |
| `AKIMAT_ADMIN`    | Read-only по всем контрактам + `POST /trips/usage`                 |
| `CONTRACTOR_ADMIN`| Читает только свои контракты/тикеты/рейсы                          |
| `TOO_ADMIN`       | Нет доступа (403)                                                  |
| `DRIVER`          | Нет доступа (403)                                                  |

