# Swagger merger

To merge a few swagger YAML files into one.

Install the command line tool first.

```shell
	go get github.com/efureev/go-swagger-merger
```

## Docker

```shell
docker pull ghcr.io/efureev/go-swagger-merger:master
```

The command below will merge `/data/swagger1.yaml` `/data/swagger2.yaml` and save result file in
the `/data/swagger.yaml`. The library supports more than two files to merge. You can add more paths to the
list `/data/swagger3.yaml`, `/data/swaggerN.yaml`.

```shell
go-swagger-merger -o ./docs/swagger.yml -i ./docs/supply.yml -i ./docs/attributes.yml -i ./docs/entities.yml
go-swagger-merger -o ./docs/swagger.yml -i ./docs/categories.yml -i ./docs/tags.yml -i ./docs/terms.yml
go-swagger-merger -o ./docs/swagger.yml -i ./docs/tags.yml -i ./docs/categories.yml -i ./docs/logs.yml -i ./docs/terms.yml -i ./docs/articles.yml

```

Attention. The order of the files is essential, and the following file overwrites the same fields from the previous
file.

Sections that work exactly:

- Servers - Exclude duplicate
- Paths
- Tags - Exclude duplicate
- Components:
    - Schemas
    - Responses
    - Parameters
