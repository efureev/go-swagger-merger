# swagger-merger

[English](README.md) · **Русский**

Слияние нескольких OpenAPI- или Swagger-документов в один — как библиотека Go
либо из командной строки.

```shell
go install github.com/efureev/go-swagger-merger/v2/cmd/swagger-merger@latest
```

```shell
swagger-merger merge -o docs/swagger.yml docs/users.yml docs/orders.yml
```

Слияние идёт по операциям и по именам определений, вывод воспроизводим
побайтово, комментарии и порядок ключей переживают round-trip, а всё
неоднозначное сообщается с указанием файла, строки и JSON-указателя, а не
разрешается молча.

- [Командная строка](#командная-строка)
- [Библиотека](#библиотека)
- [Семантика слияния](#семантика-слияния)
- [Конфликты](#конфликты)
- [Docker](#docker)
- [GitHub Actions](#github-actions)

Переходите с v1? См. [MIGRATION.ru.md](MIGRATION.ru.md).

## Командная строка

```
swagger-merger merge     [флаги] <вход...>   Слить документы
swagger-merger validate  [флаги] <вход...>   Проверить, ничего не записывая
swagger-merger version                       Вывести информацию о версии
```

Входные файлы сливаются по порядку, `-` читает стандартный ввод. Первый вход
задаёт блок `info` и версию спецификации, если `--base` не указывает другой.

| Флаг | Значение |
| --- | --- |
| `-o`, `--output` | Файл вывода либо `-` для stdout (по умолчанию `-`) |
| `--format` | `yaml` или `json`; иначе выводится из расширения `-o` |
| `--indent` | Ширина отступа (по умолчанию `2`) |
| `--on-conflict` | `error` (по умолчанию), `first` или `last` |
| `--on-conflict-section` | Политика для секции, например `schemas=first` (повторяемый) |
| `--base` | Вход, чей блок `info` и версия побеждают |
| `--sort` | Канонический порядок ключей вместо порядка входа |
| `--no-ref-validation` | Пропустить проверку локальных `$ref` |
| `--allow-version-skew` | Разрешить смешивание 3.0.x и 3.1.x |
| `--strict` | Считать предупреждения ошибками |
| `--allow-empty` | Пропускать пустые входы вместо остановки |
| `--dry-run` | Слить и отчитаться, ничего не записывая |
| `-q`, `--quiet` | Подавить диагностику и итоговую строку |
| `--log-format` | `text` (по умолчанию) или `json` для машинной обработки |

Коды выхода: `0` — слито, `1` — слияние не удалось, `2` — ошибка в командной
строке.

```shell
# Несколько документов в один, при неоднозначности побеждает первый
swagger-merger merge --on-conflict=first -o api.yaml users.yaml orders.yaml

# Вывести JSON, отсортировать и уронить сборку на чём угодно сомнительном
swagger-merger merge --sort --strict -o api.json *.yaml

# Проверка в CI без создания файла
swagger-merger validate --strict specs/*.yaml

# Конвейеры работают
cat base.yaml | swagger-merger merge - extra.yaml > out.yaml
```

## Библиотека

```shell
go get github.com/efureev/go-swagger-merger/v2
```

```go
import "github.com/efureev/go-swagger-merger/v2/merge"

res, err := merge.Merge(ctx, merge.Options{},
    merge.FileSource("users.yaml"),
    merge.FileSource("orders.yaml"),
)
if err != nil {
    return err
}
out, err := res.Document.YAML(2)
```

Нулевое значение `Options` — это и есть строгая конфигурация: конфликты
являются ошибками, локальные `$ref` проверяются, порядок ключей входа
сохраняется.

Источники берутся из файлов, памяти, потоков или `fs.FS`, поэтому встроенная
через `embed` спецификация работает без обращения к диску:

```go
//go:embed specs/*.yaml
var specs embed.FS

res, err := merge.Merge(ctx, merge.Options{SortKeys: true},
    merge.FSSource(specs, "specs/base.yaml"),
    merge.FSSource(specs, "specs/extra.yaml"),
)
```

Добавляйте документы по одному, когда набор заранее неизвестен:

```go
m := merge.New(merge.Options{OnConflict: merge.ConflictFirstWins})
for _, path := range paths {
    if err := m.Add(ctx, merge.FileSource(path)); err != nil {
        return err
    }
}
res, err := m.Result()
```

Результат несёт документ и всё, что слияние заметило по пути:

```go
for _, c := range res.Conflicts {
    log.Printf("%s: оставлено %s, отброшено %s", c.Pointer, c.Kept, c.Dropped)
}
for _, d := range res.Warnings() {
    log.Printf("%s: %s (%s)", d.Code, d.Message, d.At)
}
```

Ошибки оборачивают сентинелы, поэтому вызывающий код может ветвиться по
причине:

```go
switch {
case errors.Is(err, merge.ErrConflict):        // два входа противоречат друг другу
case errors.Is(err, merge.ErrDanglingRef):     // $ref потерял цель
case errors.Is(err, merge.ErrVersionMismatch): // 2.0 смешана с 3.x
}
```

Рендерите в YAML или JSON либо декодируйте в свои структуры:

```go
yamlBytes, err := res.Document.YAML(2)
jsonBytes, err := res.Document.JSON(2)   // порядок членов объекта сохранён
err = res.Document.Decode(&mySpecStruct)
```

Библиотека никогда не паникует, не пишет в глобальное состояние и не трогает
процесс — ни `os.Exit`, ни логирования в stdout. Это обеспечено правилом
линтера, а не соглашением.

## Семантика слияния

| Секция | Правило |
| --- | --- |
| `openapi` / `swagger` | Должны быть совместимы; побеждает старший patch внутри одного minor |
| `info`, `externalDocs`, `host`, `basePath` | Задаёт базовый документ; остальные отмечаются и игнорируются |
| `servers` | Дедупликация по `url` |
| `tags` | Дедупликация по `name` |
| `security` | Дедупликация по содержимому |
| `schemes`, `consumes`, `produces` | Объединение множеств, порядок первого появления |
| `paths`, `webhooks` | Слияние по пути, затем **по операции** |
| `components.*` | Слияние по имени определения, по всем девяти секциям компонентов |
| `definitions`, `parameters`, `responses`, `securityDefinitions` | Эквиваленты Swagger 2.0, слияние по имени |
| `x-*` и всё неизвестное | Маппинги сливаются вглубь; прочие формы подчиняются политике конфликтов |

Внутри пути `parameters` дедуплицируются по паре `(name, in)`, а `servers` — по
`url`, поэтому объединение двух файлов, каждый из которых объявляет параметр
`{id}` этого пути, даёт один параметр, а не два.

Поддержаны оба семейства спецификаций. Swagger 2.0 и OpenAPI 3.x слить между
собой нельзя; 3.0.x и 3.1.x требуют `--allow-version-skew`, поскольку в 3.1
изменилась семантика схем.

После слияния каждый локальный `$ref` резолвится по результату. Ссылка,
потерявшая цель, — это ошибка с точной локацией; именно этот способ сломаться
слияние привносит чаще всего.

Проверка ссылок учитывает позицию: `$ref` внутри `example`, `default`, `enum`,
`const` или `value` примера — это данные полезной нагрузки, а член карты имён
(схема с именем `example`, свойство с именем `$ref`) — это определение. Ни то,
ни другое ссылкой не считается.

## Конфликты

Когда два входа определяют одно и то же имя — это нормальный случай, а не
исключительный. Конфликт возникает, только если определения действительно
различаются: структурно идентичные дедуплицируются молча, поэтому общая схема
`Error`, скопированная в пять файлов, не стоит ничего.

Когда они различаются по существу, поведение по умолчанию — остановиться:

```
error: merge: conflicting definition: /components/schemas/User is defined differently in two inputs
  at /components/schemas/User
  orders.yaml:17:3
  users.yaml:42:3

hint: pass --on-conflict=first or --on-conflict=last to pick a winner,
      or --on-conflict-section schemas=first to scope it to one section.
```

Выберите победителя глобально через `--on-conflict` либо посекционно:

```shell
swagger-merger merge --on-conflict=error --on-conflict-section schemas=first ...
```

Секции, принимающие политику: `info`, `servers`, `tags`, `paths`, `webhooks`,
`components`, `schemas`, `responses`, `parameters`, `security`, `externalDocs`,
`extensions`, `root`.

`--strict` повышает предупреждения до ошибок с двумя намеренными исключениями.
Конфликт, разрешённый явным `--on-conflict`, он оставляет в покое: вы уже
сказали, что делать. То же и с замечанием о том, что `info` взят из базового
документа: собственный блок `info` есть у каждого входа, поэтому его повышение
роняло бы любое обычное слияние.

## Docker

```shell
docker pull ghcr.io/efureev/go-swagger-merger:latest

docker run --rm -v "$PWD:/data" ghcr.io/efureev/go-swagger-merger \
  merge -o /data/swagger.yml /data/users.yml /data/orders.yml
```

Образ построен на `distroless/static`, работает от непривилегированного
пользователя и публикуется для `linux/amd64` и `linux/arm64`.

## GitHub Actions

```yaml
- uses: actions/setup-go@v5
  with: { go-version: stable }
- run: go install github.com/efureev/go-swagger-merger/v2/cmd/swagger-merger@latest
- run: swagger-merger validate --strict docs/*.yaml
- run: swagger-merger merge --sort -o docs/swagger.yml docs/*.yaml
- run: git diff --exit-code -- docs/swagger.yml   # вывод воспроизводим
```

Последняя строка работает потому, что вывод детерминирован: одни и те же
входные данные дают одни и те же байты в каждом прогоне.

## Разработка

```shell
make test      # go test ./...
make race      # с детектором гонок
make lint      # golangci-lint
make fuzz      # 60 секунд фаззинга
make golden    # перегенерировать golden-фикстуры
make build     # bin/swagger-merger
```

## Лицензия

MIT — см. [LICENSE](LICENSE).
