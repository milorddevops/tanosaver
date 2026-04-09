# Tanos Saver

> "Я неизбежен" — сказал Танос, щёлкнув пальцами и удалив половину образов из Registry.

**Tanos Saver** — инструмент для резервного копирования и восстановления контейнерных образов из пространств имён Kubernetes.

## Предыстория

Вы когда-нибудь сталкивались с ситуацией, когда нужный Docker-образ внезапно исчезает из Registry? Как будто сам Танос щёлкнул пальцами и случайным образом удалил ваши образы? 

Название **Tanos Saver** появилось именно отсюда — это инструмент для борьбы с "Таносом", который непредсказуемо удаляет образы из Registry. С помощью Tanos Saver вы можете:

- Сохранить все образы из Kubernetes перед "щелчком"
- Проверить, какие образы пропали
- Восстановить отсутствующие образы из резервной копии

## Возможности

- Сохранение образов из пространств имён Kubernetes в локальное хранилище или S3
- Проверка отсутствующих образов в Registry
- Восстановление отсутствующих образов из резервной копии
- **Автоматическое спасение образов с нод Kubernetes** — если образ недоступен в Registry, он экспортируется напрямую с ноды через `crictl` (только c опций сохранния в s3) 
- Не требует Docker-демона (написан на чистом Go)
- Поддержка локальной файловой системы и S3-совместимых хранилищ

## Установка

### Из исходного кода

```bash
CGO_ENABLED=0 go build -tags "containers_image_openpgp exclude_graphdriver_btrfs exclude_graphdriver_devicemapper" -o tanos-saver ./cmd/tanos-saver
```

### Docker

```bash
docker build -t tanos-saver .
```

## Конфигурация

### Kubernetes

| Переменная | Описание | По умолчанию |
|------------|----------|--------------|
| `KUBECONFIG` | Путь к файлу kubeconfig | `~/.kube/config` |
| `NAMESPACES` | Список пространств имён через запятую | **обязательно** |
| `RESCUE_IMAGE` | Образ для спасения образов с нод | `tanos-rescue:latest` |

### Registry

| Переменная | Описание | По умолчанию |
|------------|----------|--------------|
| `REGISTRY_URL` | URL Registry | **обязательно** |
| `REGISTRY_USER` | Имя пользователя | из docker config |
| `REGISTRY_PASSWORD` | Пароль | из docker config |
| `REGISTRY_INSECURE` | Пропустить проверку TLS | `false` |

### Хранилище

| Переменная | Описание | По умолчанию |
|------------|----------|--------------|
| `STORAGE_TYPE` | Тип хранилища: `local` или `s3` | `local` |
| `STORAGE_PATH` | Путь к локальному хранилищу | `/var/lib/tanos-backups` |

#### S3 хранилище

| Переменная | Описание | По умолчанию |
|------------|----------|--------------|
| `S3_ENDPOINT` | URL S3-эндпоинта | - |
| `S3_BUCKET` | Имя бакета | - |
| `S3_ACCESS_KEY` | Access key | - |
| `S3_SECRET_KEY` | Secret key | - |
| `S3_REGION` | Регион S3 | `us-east-1` |

## Использование

```bash
# Сохранить образы из пространств имён в резервную копию
tanos-saver save

# Проверить, какие образы отсутствуют в Registry
tanos-saver check

# Восстановить отсутствующие образы
tanos-saver restore

# Показать список сохранённых образов
tanos-saver list
```

## Примеры

### Сохранение образов из нескольких пространств имён

```bash
export NAMESPACES=dev,staging,prod
export REGISTRY_URL=registry.example.com
export REGISTRY_USER=admin
export REGISTRY_PASSWORD=secret
export STORAGE_PATH=/data/backups

tanos-saver save
```

### Восстановление отсутствующих образов

```bash
tanos-saver restore
```

### Использование S3 хранилища

```bash
export STORAGE_TYPE=s3
export S3_ENDPOINT=https://s3.example.com
export S3_BUCKET=image-backups
export S3_ACCESS_KEY=minioadmin
export S3_SECRET_KEY=minioadmin

tanos-saver save
```

### Cron-задачи

```cron
# Сохранять образы каждую ночь в 2:00
0 2 * * * tanos-saver save >> /var/log/tanos-saver.log 2>&1

# Проверять и восстанавливать каждый час
0 * * * * tanos-saver restore >> /var/log/tanos-saver.log 2>&1
```

## Как это работает

1. **save**: Подключается к Kubernetes, получает список всех рабочих нагрузок (Deployments, StatefulSets, DaemonSets, CronJobs и др.), извлекает уникальные ссылки на образы, скачивает их с помощью `containers/image` и сохраняет в хранилище как tar-файлы.

2. **check**: Загружает манифест резервной копии, проверяет наличие каждого образа в Registry с помощью HEAD-запросов, сообщает, какие образы отсутствуют.

3. **restore**: Загружает манифест резервной копии, проверяет Registry, восстанавливает отсутствующие образы, загружая их из tar и отправляя в Registry.

## Спасение образов с нод (Rescue)

Если при сохранении образов какой-то образ недоступен в Registry (например, был удалён), Tanos Saver автоматически попытается спасти его напрямую с ноды Kubernetes:

1. Находит ноду, на которой запущен контейнер с нужным образом
2. Создаёт привилегированный Job на этой ноде
3. Job монтирует `/run/containerd` и `/var/lib/containerd` с хоста
4. Экспортирует образ через `crictl export`
5. Загружает tar-файл в S3-хранилище

**Требования:**
- S3-хранилище (rescue не работает с локальным хранилищем)
- Специальный rescue-образ с инструментами `crictl`, `ctr`, `mc`

**Сборка rescue-образа:**
```bash
# Собрать локально
make docker-build-rescue

# Собрать и загрузить в Registry
make docker-push-rescue RESCUE_IMAGE=registry.example.com/infrastructure/tanos-rescue:1.0
```

**Использование своего rescue-образа:**
```bash
export RESCUE_IMAGE=registry.example.com/infrastructure/tanos-rescue:1.0
```
