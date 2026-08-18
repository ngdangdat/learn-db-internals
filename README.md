# learn-db-internals

A small Go playground for working through [*Database Internals*](docs/database_internals.pdf) by Alex Petrov.

The project starts deliberately small. Each concept from the book can be added as a focused package with tests as we reach it.

The first experiment is a durable in-memory key/value store. It writes mutations to a synced write-ahead log, can checkpoint the current state to a snapshot, and recovers by loading the snapshot and replaying the remaining log.

The second experiment is an in-memory row store with a fixed schema. Its secondary indexes store primary keys, so a secondary lookup resolves through the primary index before reading the row. This models the primary-index indirection tradeoff described in the book.

## Requirements

- Go 1.26 or newer
- Docker, for the recommended development environment
- VS Code with the Dev Containers extension, when using the dev container

## Development container

Open the repository in VS Code and choose **Reopen in Container**. The container installs the Go toolchain, configures Zsh with Oh My Zsh as the default terminal, and runs the test suite after it is created. The `gst` helper runs `git status`.

At this stage, the whole stack is the Go command itself; no external services are required yet.

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
.devcontainer/             Development container definition
cmd/learn-db-internals/    Command-line entry point
docs/                       Book and study material
rowstore/                   In-memory row store with primary-key indirection
store/                      Durable in-memory store experiment
```

These stores are intentionally learning implementations, not production databases. The row store has no SQL parser, transactions, or persistence yet. The durable key/value store currently uses synchronous writes and explicit checkpoints; asynchronous batching, sorted on-disk indexes, checksums, and transactions are deferred.
