# Coding Agent Instructions

> **Declaration ordering**: Keep declarations in alphabetical order within each file unless there is a compelling reason to do otherwise (e.g., a type must be declared before its methods, or a logical grouping improves readability).

> **Test style**: Tests should use `r := require.New(t)` from `github.com/stretchr/testify/require` to create a short-named asserter, then call `r.NoError(...)`, `r.True(...)`, `r.Equal(...)`, etc. instead of manual `if`/`t.Fatal` patterns.

> **Doc comments on unexported types**: Do not add trivial "Method implements Interface" comments on methods of unexported types. The interface contract is documented on the interface itself and discoverable via the compile-time check (e.g., `var _ Context = (*ctx)(nil)`).

> **Compile-time interface checks**: Comments on compile-time interface checks (e.g., `var _ Context = (*ctx)(nil)`) are unnecessary. The declaration is self-explanatory.

> **Ignored returns**: Any code that intentionally ignores an `error` should use an explicit assignment to the blank identifier (e.g., `_ = foo()`). Do not use `//nolint:errcheck` or similar annotations to suppress the warning.

> **Unused arguments**: Function arguments that exist solely to fulfill an interface or type contract but are not used in the body should be named `_` (e.g., `func(_ Context) ...`). This signals that the parameter is intentionally unused.

> **Locked-method naming**: Any unexported method on a type that requires the caller to hold a lock must encode the requirement in its name using camelCase (e.g., `softStopLocked`, `hardStopLocked`, `runDeferredLocked`).

> **Locking in tests**: It is reasonable to call locked methods (e.g., `softStopLocked`, `hardStopLocked`) and to read/write guarded state without holding the lock when a test does not use any concurrency. Omitting unnecessary lock/unlock calls keeps tests concise and makes it clear that no concurrent access is involved.

> **Race checker**: All tests should be run with the race detector enabled (`-race` flag). This ensures that concurrent access patterns are validated during development.

> **Test timeout**: All `go test` invocations should use `-timeout 10s`. The stopper lifecycle is designed to complete quickly; a test that exceeds 10 seconds almost certainly has a deadlock or missed stop signal and should fail fast rather than hang.

> **Progress tracking**: Mark sections as completed along the way. Append ` ✅` to the heading of each section once all of its sub-tasks are done and verified.

> **Copyright hygiene**: Every `.go` file must begin with a copyright header. Files containing code derived from the original Cockroach-authored commits (`2f549bd`, `33e29fb`) must retain `Copyright 2023 The Cockroach Authors` above the Bob Vawter line. New files that contain only original work use `Copyright <year> Bob Vawter (bob@vawter.org)` alone. Both forms include `SPDX-License-Identifier: Apache-2.0`. When creating a new `.go` file, always add the appropriate copyright header as the very first lines.

> **README.md**: Keep `README.md` in sync with the package's public API. When adding, removing, or renaming exported types, functions, or sub-packages, update the Features list, Quick Start snippet, and Examples table so they stay accurate. If a new sub-package is introduced, add a link to its `pkg.go.dev` page.

> **doc.go**: The top-level `doc.go` is the canonical package documentation shown on `pkg.go.dev`. Update it whenever the public API surface changes — new exported symbols, removed APIs, or renamed concepts — so the overview, code examples, and section headings remain correct.
