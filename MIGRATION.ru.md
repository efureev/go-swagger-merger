# Переход с v1 на v2

[English](MIGRATION.md) · **Русский**

v2 — это новый путь модуля, поэтому ничего не ломается, пока вы сами не решите
обновиться. v1 по-прежнему резолвится по адресу
`github.com/efureev/go-swagger-merger`; v2 живёт по адресу
`github.com/efureev/go-swagger-merger/v2`.

## Зачем переписывать

v1 в общем случае выдавала неверный документ:

- **Терялись операции.** Слияние `paths` было закомментировано, поэтому элемент
  пути заменялся целиком. Слияние `{/users: get}` с `{/users: post}` давало
  документ, в котором остался только `post`, — и ничего об этом не сообщалось.
- **Вывод был невоспроизводим.** Документ проходил round-trip через
  `map[string]any`, поэтому порядок ключей брался из случайной итерации map в
  Go. Два прогона на одних и тех же входных данных давали разные файлы, что
  делает закоммиченный артефакт бесполезным для diff.
- **Второе слияние в одном процессе молча теряло данные.** Дедупликация
  серверов и тегов использовала map уровня пакета, поэтому второй созданный в
  процессе `Merger` считал все серверы и теги уже присутствующими.
- **Обычный ввод ронял программу.** Непроверенные type assertions в восьми
  местах означали, что пустой файл, `servers: null` или сервер без `url`
  вызывали панику.
- **Конфликты были невидимы.** Два файла, определяющих `User` по-разному,
  оставляли один из них произвольно и без предупреждения.

## Командная строка

Форма v1 продолжает работать:

```shell
swagger-merger -o ./docs/swagger.yml -i ./docs/a.yml -i ./docs/b.yml
```

Форма v2 короче, а `-o` теперь по умолчанию пишет в stdout:

```shell
swagger-merger merge -o ./docs/swagger.yml ./docs/a.yml ./docs/b.yml
```

Что изменилось:

| v1 | v2 |
| --- | --- |
| Бинарь `go-swagger-merger` | Бинарь `swagger-merger` |
| Без подкоманд | `merge`, `validate`, `version`, `help` |
| `-o` требовал файл | `-o` по умолчанию `-` (stdout) |
| Конфликты разрешались молча | Конфликты — ошибки; `--on-conflict=first` возвращает поведение, близкое к v1 |
| `panic` со stack trace | Сообщение об ошибке и код выхода `1` |
| Всегда YAML | `--format json` либо вывод из расширения `-o` |
| Без валидации | Локальные `$ref` проверяются; `validate` сообщает больше |

Единственный намеренный слом совместимости — политика конфликтов по умолчанию.
Если вы полагались на то, что v1 тихо оставляет первое определение:

```shell
swagger-merger merge --on-conflict=first -o out.yml a.yml b.yml
```

Будьте готовы, что при первом же запуске это вскроет реальные проблемы.

## Библиотека

```go
// v1
import "github.com/efureev/go-swagger-merger/merger"

m := merger.NewMerger()
for _, f := range files {
    if err := m.AddFile(f); err != nil {
        panic(err)
    }
}
err := m.Save("out.yaml")
```

```go
// v2
import "github.com/efureev/go-swagger-merger/v2/merge"

res, err := merge.Merge(ctx, merge.Options{}, merge.FileSources(files...)...)
if err != nil {
    return err
}
out, err := res.Document.YAML(2)
if err != nil {
    return err
}
return os.WriteFile("out.yaml", out, 0o644)
```

| v1 | v2 |
| --- | --- |
| `merger.NewMerger()` | `merge.New(opts)` либо `merge.Merge(ctx, opts, srcs...)` |
| `m.AddFile(path)` | `m.Add(ctx, merge.FileSource(path))` |
| `m.Save(path)` | `res.Document.YAML(2)`, запись — за вами |
| `m.Swagger` (`map[string]any`) | `res.Document.Node()` (`*yaml.Node`) либо `Document.Decode(&v)` |
| — | `res.Conflicts`, `res.Diagnostics`, `res.Warnings()` |

О чём ещё стоит знать:

- **Ничего не паникует.** Любой сбой — это ошибка, оборачивающая сентинел
  (`merge.ErrConflict`, `merge.ErrDanglingRef`, `merge.ErrVersionMismatch`,
  `merge.ErrInvalidDocument`, `merge.ErrNoInput`, `merge.ErrStrict`) и несущая
  файл, строку, колонку и JSON-указатель.
- **Нет глобального состояния.** Независимые значения `Merger` не влияют друг
  на друга, конкурентные слияния безопасны.
- **Комментарии и порядок ключей сохраняются**, потому что документ остаётся
  `*yaml.Node`, а не превращается в map.
- **Вывод детерминирован.** Одни и те же входные данные — одни и те же байты.
- **`merger.ToString` удалён.** Это был экспортированный хелпер с единственным
  внутренним вызовом, который в нём не нуждался.

## Docker

Образ v1 не объявлял `ENTRYPOINT`, поэтому `docker run` ничего не делал. В v2:

```shell
docker run --rm -v "$PWD:/data" ghcr.io/efureev/go-swagger-merger \
  merge -o /data/swagger.yml /data/a.yml /data/b.yml
```

Образ теперь построен на `distroless/static`, работает от непривилегированного
пользователя и публикуется для `linux/amd64` и `linux/arm64`.

## Чего v2 не делает

Резолв внешних `$ref` (`./common.yaml#/components/schemas/Error`) и
автоматическое переименование конфликтующих определений — вне области действия.
Внешние ссылки сообщаются и остаются нетронутыми; конфликты выносятся наружу,
чтобы вы разрешили их политикой.
