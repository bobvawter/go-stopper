# Competitive Market Analysis: `stopper` v2 vs. Go Concurrency Libraries

This document provides a comparative analysis of `vawter.tech/stopper/v2` against other popular structured concurrency and task-management libraries in the Go ecosystem.

## Abstract

`stopper` v2 is a high-level framework for **structured, observable concurrency** in Go. It extends the standard library `context.Context` with a hierarchical task model, two-phase graceful shutdown, and deep runtime visibility. While other libraries focus on primitive synchronization or safety, `stopper` provides a complete "batteries-included" ecosystem for building robust, long-running services.

## The Competitive Landscape

### 1. The Standard: `golang.org/x/sync/errgroup`

`errgroup` is the de-facto standard for basic structured concurrency in Go. It provides a simple way to wait for a group of tasks and cancel them all if any one of them returns an error.

*   **Strengths**: Minimal overhead, zero dependencies, simple API.
*   **Weaknesses**: 
    *   **Single-phase shutdown**: Only supports context cancellation (`Done()`). No distinction between a "soft-stop" (drain) and a "hard-cancel" (kill).
    *   **Zero Observability**: No built-in way to see which tasks are currently running or where they were started.
    *   **Strict Signature**: Tasks must be `func() error`. Requires manual wrapping for any other signature.
    *   **No Panic Safety**: A panic in an `errgroup` goroutine will crash the entire process unless the user adds a recovery wrapper.
*   **When to use**: Short-lived, performance-critical parallel tasks where the extra features of `stopper` would be overkill.

### 2. The Ergonomic Utility: `github.com/sourcegraph/conc`

`conc` is a modern library that focuses on making concurrent code safer and easier to read. It provides "better" versions of `sync.WaitGroup`, pools, and iterator helpers.

*   **Strengths**: 
    *   **Panic Safety**: Automatically catches panics and includes the goroutine stack trace in the error.
    *   **Iterators**: Excellent support for `ForEach` and `Map` on slices.
    *   **Clean API**: Object-oriented API (`pool.New()`) that feels very modern.
*   **Weaknesses**:
    *   **Flat Task Model**: Lacks a hierarchical context-based tree.
    *   **Lifecycle Limitations**: Does not support two-phase graceful shutdown.
    *   **Observability**: While it provides good error details, it lacks real-time task tree inspection and deep `runtime/trace` integration.
*   **When to use**: General-purpose task parallelization and replacing standard library `WaitGroup`s with something safer.

### 3. The Service Lifecycle Orchestrator: `github.com/oklog/run`

`oklog/run` is a minimalist library for wiring together the disparate actors of a Go service (e.g., HTTP server, metrics exporter, signal listener).

*   **Strengths**: 
    *   **Simple Actor Model**: Every actor has an `Execute` and an `Interrupt` function.
    *   **First-Exit Wins**: If any actor returns, all others are interrupted.
*   **Weaknesses**:
    *   **Primitive API**: Requires manual wiring of `Execute`/`Interrupt` pairs.
    *   **No Task Metadata**: No concept of task names, durations, or hierarchy.
    *   **Single-use**: Designed for top-level `main.go` orchestration rather than per-request concurrency.
*   **When to use**: Wiring up the main entry point of a service where you have several independent long-running loops.

### 4. The Legacy Ancestor: `github.com/facebookgo/stopper`

An older library that provided a way to broadcast a stop signal to multiple goroutines.

*   **Strengths**: Simple "broadcast" mechanism.
*   **Weaknesses**: 
    *   **Obsolete**: Predates the modern use of `context.Context` as the standard for cancellation.
    *   **Manual Wiring**: Requires goroutines to explicitly check the stopper object.
*   **When to use**: Legacy codebases that haven't migrated to `context.Context`.

## Why `stopper` v2 is Different

`stopper` is not just a tool; it's a **concurrency framework**. It bridges the gap between low-level synchronization and high-level service observability.

### Differentiator 1: Two-Phase Graceful Shutdown
Most Go libraries treat "stop" and "cancel" as the same thing. `stopper` distinguishes between them:
1.  **Soft-Stop (`Stopping()`)**: Signals tasks to begin draining, flushing buffers, and finishing in-flight work.
2.  **Hard-Cancel (`Done()`)**: Triggered after a configurable grace period, ensuring the process eventually exits.
This separation is critical for high-availability systems that cannot afford to drop requests on restart.

### Differentiator 2: Deep Runtime Observability
`stopper` provides a level of visibility that is unique in the Go ecosystem:
*   **`TaskTree`**: A human-readable, real-time visualization of every active task, where it was started, and how long it's been running.
*   **Automatic `runtime/trace` Integration**: Every context and task is automatically wrapped in a `trace.Task`, making the stopper hierarchy immediately visible in Go execution traces with zero extra code.

### Differentiator 3: Batteries-Included Ecosystem
`stopper` ships with sub-packages that solve common concurrency problems:
*   **`limit`**: First-class middleware for max concurrency and token-bucket rate limiting.
*   **`retry`**: Composable backoff/retry middleware.
*   **`seq`**: Bounded-concurrency iterators for Go 1.23+ `iter.Seq` types.
*   **`linger`**: Test helpers that detect goroutine leaks and non-terminating tasks.

### Differentiator 4: Generic Task Adaptation
`stopper.Go` and `stopper.Call` accept any function signature from `func()` to `func(stopper.Context) error`. This removes the "ceremony" of manually wrapping functions to fit a specific interface.

## Comparison Matrix

| Feature | `stopper` v2 | `sync/errgroup` | `conc` | `oklog/run` |
| :--- | :---: | :---: | :---: | :---: |
| **Hierarchical Task Tree** | ✅ | ❌ | ❌ | ❌ |
| **Two-Phase Shutdown** | ✅ | ❌ | ❌ | ❌ |
| **Task Observability (Tree)**| ✅ | ❌ | ❌ | ❌ |
| **`runtime/trace` Support** | ✅ | ❌ | ❌ | ❌ |
| **Built-in Panic Safety** | ✅ | ❌ | ✅ | ❌ |
| **Middleware Support** | ✅ | ❌ | ❌ | ❌ |
| **Built-in Rate/Concurrency Limit**| ✅ | ❌ | ✅ | ❌ |
| **Built-in Retries** | ✅ | ❌ | ❌ | ❌ |
| **Go 1.23 Iterators** | ✅ | ❌ | ✅ | ❌ |

## Conclusion

Choose **`sync/errgroup`** if you are writing a performance-critical library or a simple CLI where you only need a basic `Wait()` and `Cancel()`.

Choose **`conc`** if you want a safer, more ergonomic way to handle parallel work on slices and streams without the weight of a full lifecycle manager.

Choose **`oklog/run`** for simple, top-level service orchestration where each component is an independent, opaque actor.

Choose **`stopper` v2** if you are building **long-running, observable services** that require robust graceful shutdown, deep runtime visibility, and sophisticated task management (limiting, retrying, and tracing). It is the "enterprise-grade" choice for structured concurrency in Go.
