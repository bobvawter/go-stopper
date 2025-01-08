# Golang Task Lifecycle Management

[![Go Reference](https://pkg.go.dev/badge/vawter.tech/stopper.svg)](https://pkg.go.dev/vawter.tech/stopper)

```shell
go get vawter.tech/stopper
```

This package contains a utility for gracefully terminating long-running tasks within a Go program.
A `stopper.Context` extends the stdlib `context.Context` API with a soft-stop signal and includes
task-launching APIs similar to `sync.WaitGroup` or `sync.ErrGroup`. This API supports nested contexts
for use-cases where tasks may be hierarchical in nature.

## Project History

This repository was extracted from `github.com/cockroachdb/field-eng-powertools` using the command
`git filter-repo --subdirectory-filter stopper --path LICENSE` by the code's original author.
