# Swagger merger

To merge a few swagger YAML files into one.

Install the command line tool first.

	go get github.com/efureev/go-swagger-merger

The command below will merge ``/data/swagger1.yaml`` ``/data/swagger2.yaml`` and save result file in
the ``/data/swagger.yaml``. The library supports more than two files to merge. You can add more paths to the
list ``/data/swagger3.yaml``, ``/data/swaggerN.yaml``.

```shell
go-swagger-merger -o ./docs/swagger.yml ./docs/supply.yml ./docs/attributes.yml ./docs/entities.yml
go-swagger-merger -o ./docs/servers.yml ./docs/server1.yml ./docs/server2.yml

```

Attention. The order of the files is essential, and the following file overwrites the same fields from the previous
file.

Sections:

- Servers - Exclude duplicate
- Paths
- Tags - Exclude duplicate
- Components:
    - Schemas
    - Responses
    - Parameters
