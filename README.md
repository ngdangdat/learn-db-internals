# learn-db-internals

A small Go playground for working through [*Database Internals*](docs/database_internals.pdf) by Alex Petrov.

The project starts deliberately small. Each concept from the book can be added as a focused package with tests as we reach it.

## Requirements

- Go 1.23 or newer

## Run

```sh
go run ./cmd/learn-db-internals
```

## Test

```sh
go test ./...
```

## Layout

```text
cmd/learn-db-internals/  Command-line entry point
 docs/                    Book and study material
```

No database engine is implemented yet; this is just the initial project scaffold.
