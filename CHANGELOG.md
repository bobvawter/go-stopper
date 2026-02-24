# Changelog

## v2.0.0-alpha.next
  * Update `seq` package iterators to only cancel on hard-stop.

## v2.0.0-alpha.1
  * Complete API overhaul. See the [migration guide](doc/migration.md)
    for a detailed list of changes from v1.

## v1.2.0
  * Add `Context.StopOnIdle()` for bounded task pools.
  * Add `stopper.Harden()` to export soft-stop behavior as `context.Context`.
  * Add `stopper.Fn()` convenience adaptor.

## v1.1.0
  * Add `stopper.Invoker` type to allow users to decorate function calls
  * Add `stopper.WithInvoker()` constructor
  * Add `linger` subpackage with test helpers

## v1.0.2
  * No API changes
  * Maintenance and documentation release

## v1.0.1
  * Add `Context.With()` method.
  * Add `stopper.StopOnReceive()` function.

## v1.0.0
  * Initial module release