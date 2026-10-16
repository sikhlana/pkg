# log Specification

## Purpose

Builds an app's structured logger from developer wiring alone: a level from debug and quiet switches, JSON or text output to standard error or a chosen writer, pluggable text rendering, and middleware that can enrich, rewrite or drop every record without breaking slog's grouping rules.

## Requirements

### Requirement: The level follows the debug and quiet switches

The logger SHALL write records at Info and above when neither switch is set, at Debug and above when debug is set, and at Warn and above when quiet is set. When both are set, debug SHALL win. The level SHALL hold whichever text handler renders the records, including one that ignores the level it's given.

#### Scenario: Neither switch
- **WHEN** neither switch is set and the app logs at Debug and at Info
- **THEN** only the Info record is written

#### Scenario: Debug
- **WHEN** debug is set and the app logs at Debug
- **THEN** the record is written

#### Scenario: Quiet
- **WHEN** quiet is set and the app logs at Info, Warn and Error
- **THEN** the Warn and Error records are written and the Info record isn't

#### Scenario: Both switches
- **WHEN** debug and quiet are both set and the app logs at Debug
- **THEN** the record is written

#### Scenario: A text handler that ignores the level
- **WHEN** the format is text, the app's text handler writes every record it's given, neither switch is set, and the app logs at Debug
- **THEN** nothing is written

### Requirement: Output defaults to JSON on standard error

With no wiring, the logger SHALL write each record as one JSON object on its own line to standard error. A writer the app supplies SHALL receive every record instead. A nil writer, or a nil pointer of a writer type, SHALL count as none supplied.

#### Scenario: No wiring
- **WHEN** the app builds the logger with no wiring and logs at Info
- **THEN** standard error receives one line holding a JSON object with the record's time, level, message and source location

#### Scenario: A supplied writer
- **WHEN** the app supplies a writer and logs a record
- **THEN** the record is written to that writer and nothing to standard error

#### Scenario: A nil writer
- **WHEN** the app supplies a nil writer, or a nil `*os.File`, and logs a record
- **THEN** building the app doesn't fail and the record is written to standard error

### Requirement: The format is JSON or text

The format SHALL be JSON or text. Its text form SHALL be `json` or `text`, so a flag can parse it, and decoding any other name SHALL fail with an error matching the module's unknown-format error. Building a logger with a format value outside the two SHALL fail with the same error.

#### Scenario: Decoding a name
- **WHEN** the text `text` is decoded into a format
- **THEN** the format is text, and encoding it gives `text` back

#### Scenario: Decoding an unknown name
- **WHEN** the text `yaml` is decoded into a format
- **THEN** decoding fails with an error matching the unknown-format error

#### Scenario: A format value outside the two
- **WHEN** the app builds a logger with a format value converted from an integer that names neither format
- **THEN** building fails with an error matching the unknown-format error

### Requirement: Text rendering is pluggable

With the text format, the logger SHALL render records with the text handler the app supplies, giving it the destination, the level and whether to report the source location. Without one, it SHALL render with the standard library's text handler, without colour. With the JSON format, a supplied text handler SHALL go unused.

#### Scenario: A supplied text handler
- **WHEN** the format is text, the app supplies a text handler, and logs a record
- **THEN** the handler is built with the destination and the level, and the record reaches it

#### Scenario: No text handler
- **WHEN** the format is text, the app supplies no text handler, and logs a record with a string attribute
- **THEN** the destination receives a plain `key=value` line with no colour codes

#### Scenario: JSON ignores the text handler
- **WHEN** the format is JSON and the app supplies a text handler
- **THEN** records are written as JSON and the text handler is never built

### Requirement: The source location depends on the format and debug

JSON records SHALL carry the source location of the logging call. With the text format, the text handler SHALL be asked to report the source location only when debug is set. A record created without a source location SHALL carry none in either format.

#### Scenario: JSON
- **WHEN** the format is JSON, neither switch is set, and the app logs a record
- **THEN** the record carries the file and line of the logging call

#### Scenario: Text without debug
- **WHEN** the format is text and debug isn't set
- **THEN** the text handler is told not to report the source location

#### Scenario: Text with debug
- **WHEN** the format is text and debug is set
- **THEN** the text handler is told to report the source location

#### Scenario: A record without a source location
- **WHEN** code hands the logger's handler a record created without a source location
- **THEN** the written record has no source location

### Requirement: Middleware sees every enabled record

Middleware the app registers SHALL run on every record that passes the level, with the context the record was logged with. That SHALL hold whether the record came from the logger, from a logger derived with attributes or groups, or straight from the logger's handler. The record a middleware sees SHALL hold every attribute it will be written with, including those a derived logger attached. A middleware SHALL be able to add attributes, change the record, or drop it by not passing it on. A record below the level SHALL reach no middleware.

#### Scenario: The context reaches the middleware
- **WHEN** a middleware adds an attribute from a context value and the app logs with a context carrying that value
- **THEN** the written record carries the attribute

#### Scenario: A derived logger
- **WHEN** the app logs through a logger derived with an attribute and a group
- **THEN** the middleware runs on the record

#### Scenario: A record handed to the handler
- **WHEN** code builds a record and hands it straight to the logger's handler
- **THEN** the middleware runs on it

#### Scenario: Attributes attached to a derived logger
- **WHEN** a middleware replaces every `token` attribute's value and the app logs through a logger derived with a `token` attribute
- **THEN** the written record carries the replaced value, not the original

#### Scenario: Dropping a record
- **WHEN** a middleware doesn't pass a record on
- **THEN** nothing is written for it

#### Scenario: Below the level
- **WHEN** neither switch is set and the app logs at Debug
- **THEN** no middleware runs

### Requirement: Middleware runs in the order given at registration

Middleware SHALL run in ascending order of the order the app gives at registration, the lowest nearest the caller, so each sees what lower ones passed on. Middleware registered with the same order SHALL run in an unspecified order. Middleware registered as a value and as a constructor SHALL be ordered together.

#### Scenario: A higher order sees a lower one's work
- **WHEN** a middleware at order 10 adds an attribute and one at order 20 records what it receives
- **THEN** the order-20 middleware receives the record with that attribute

#### Scenario: A lower order drops
- **WHEN** a middleware at order 10 drops a record
- **THEN** the middleware at order 20 never sees it

#### Scenario: Values and constructors together
- **WHEN** a middleware registered as a constructor has order 15 and middleware registered as values have orders 10 and 20
- **THEN** they run in the order 10, 15, 20

### Requirement: Attributes a middleware adds land at the top level

An attribute a middleware adds SHALL be written at the top level of the record, outside any group the caller opened. The caller's own attributes and groups SHALL keep their structure and order.

#### Scenario: Inside an open group
- **WHEN** the app logs through a logger with the group `http` open, passing an integer `status`, and a middleware adds a string `request_id`
- **THEN** the record has `status` under `http` and `request_id` at the top level

#### Scenario: Attributes before and after a group
- **WHEN** the app derives a logger with the attribute `server`, then the group `http`, then the attribute `route`, and logs through it
- **THEN** `server` is at the top level and `route` is under `http`

#### Scenario: Plain and grouped attributes on a derived logger
- **WHEN** the app derives a logger with `a`, a group `g`, and `b`, derives another from it with `server`, and logs `c` through the second
- **THEN** the record holds `a`, `g`, `b`, `server` and `c`, in that order

### Requirement: Groups with the same key merge

Before a record is written, groups that share a key with a sibling, at any depth, SHALL be merged into one group at the position of the first, keeping their attributes' order. Groups attached to a derived logger SHALL merge the same way, including inside a group the logger opened. Plain attributes that share a key SHALL be written as they are.

#### Scenario: A middleware's group and the caller's
- **WHEN** the app logs a record with the group `http` holding `method`, and a middleware adds the group `http` holding `request_id`
- **THEN** the record has one `http` group holding `method` then `request_id`

#### Scenario: Nested groups
- **WHEN** a record holds `network.client.ip` and a middleware adds `network.client.geo`
- **THEN** the record has one `network` group with one `client` group holding both

#### Scenario: A group attached to a derived logger
- **WHEN** the app derives a logger with the group `http` holding `route` and logs a record with the group `http` holding `status_code`
- **THEN** the record has one `http` group holding both

#### Scenario: Inside an open group
- **WHEN** the app opens the group `req`, derives a logger with the group `http` holding `route`, and logs a record with the group `http` holding `status_code`
- **THEN** the record has one `req` group holding one `http` group holding both

#### Scenario: Plain duplicates
- **WHEN** a record carries `request_id` and a middleware adds another `request_id`
- **THEN** both are written

### Requirement: fx events follow the level

The module's fx event logger SHALL report fx's lifecycle events at Debug and its failures at Error, so ordinary events show only when debug is set and failures show in every mode, quiet included.

#### Scenario: Default
- **WHEN** an app with the event logger starts and stops with neither switch set
- **THEN** no fx event records are written

#### Scenario: Debug
- **WHEN** the same app runs with debug set
- **THEN** records for fx's provides, invokes and lifecycle hooks are written

#### Scenario: A failure under quiet
- **WHEN** an app with the event logger has a start hook that fails and quiet is set
- **THEN** a record of the failure is written at Error

### Requirement: The line-writer logs each line as a record

The line-writer SHALL write one record per newline-terminated line written to it, at its level. It SHALL strip a carriage return before the newline, skip empty lines, and write a partial last line when it's closed. Every run of 64 KiB without a newline SHALL become a record of its own, cut after the last complete UTF-8 character, whether the run arrives in one write or many, so a stream without newlines can't grow its buffer without bound. When its level is disabled, it SHALL write nothing and still accept every write. Writes SHALL succeed whatever the handler returns, and the writer SHALL be safe for concurrent use.

#### Scenario: Two lines
- **WHEN** `one\ntwo\n` is written
- **THEN** two records are written, with the messages `one` and `two`

#### Scenario: Carriage returns and empty lines
- **WHEN** `one\r\n\r\ntwo\n` is written
- **THEN** two records are written, with the messages `one` and `two`

#### Scenario: A partial line on close
- **WHEN** `tail` is written without a newline and the writer is closed
- **THEN** one record with the message `tail` is written

#### Scenario: A long line
- **WHEN** 100 KiB of multi-byte characters is written without a newline and the writer is closed
- **THEN** several records are written, each message is valid UTF-8, and together they hold every byte written

#### Scenario: A long line in one write
- **WHEN** 100 KiB of multi-byte characters followed by a newline is written in one write, and again in 32 KiB writes
- **THEN** both give the same records, each at most 64 KiB and valid UTF-8

#### Scenario: A disabled level
- **WHEN** a writer at Debug writes a line and debug isn't set
- **THEN** nothing is written and the write reports every byte accepted

### Requirement: Line-writer records carry where the writer was created

Each record from the line-writer SHALL carry the source location of the code that created the writer, and SHALL go through the logger's handler with the context the writer was created with, so middleware runs on it.

#### Scenario: The source location
- **WHEN** a writer created on a given line of a JSON-format app writes a line
- **THEN** the record's source location is that file and line

#### Scenario: Context and middleware
- **WHEN** a writer is created with a context carrying a value and a middleware adds an attribute from that value
- **THEN** each record from the writer carries the attribute

### Requirement: The module provides line-writers

The module SHALL provide a way to create line-writers that log through the app's logger, for code that takes it from the graph instead of the logger. Each writer it creates SHALL log at the level and with the context it's given, carry the attributes it's given on every record and no others, and report the source location of the code that created it. Its records SHALL otherwise behave as the line-writer's.

#### Scenario: Created from the graph
- **WHEN** code takes the provider from an app with the module, creates a writer at Warn with a context carrying a value and a `stream` attribute, and a middleware adds an attribute from that value
- **THEN** each record from the writer is at Warn, carries `stream` and the middleware's attribute, and its source location is the line that created the writer

#### Scenario: Attributes stay with their writer
- **WHEN** code creates one writer with no attributes and another with a `stream` attribute, and each writes a line
- **THEN** only the second writer's record carries `stream`

### Requirement: The tint handler colours only on a terminal

The tint text handler SHALL colour its output only when the destination is a terminal and `NO_COLOR` is unset or empty. It SHALL honour the level and source setting it's given.

#### Scenario: Not a terminal
- **WHEN** the destination is a buffer
- **THEN** the output has no colour codes

#### Scenario: NO_COLOR
- **WHEN** `NO_COLOR` is set to a non-empty value
- **THEN** the output has no colour codes, whatever the destination

#### Scenario: Level and source
- **WHEN** it's built at Warn with source reporting on, and a Warn record arrives
- **THEN** the line includes the record's source location, and an Info record writes nothing

### Requirement: The charm handler renders groups as qualified keys

The charm text handler SHALL render attributes inside groups under dot-qualified keys, as the standard library's text handler does, not as a prefix on the message. It SHALL label a level between the standard ones as slog names it from Debug-8 to Error+8, and with the nearest standard level's label beyond that, and never as fatal. It SHALL honour the level and source setting it's given, and follow slog's handler rules.

#### Scenario: A group
- **WHEN** a record with the message `served` and the integer `status` 200 is logged under the group `http`
- **THEN** the line holds `http.status=200`, and the message carries no `http` prefix

#### Scenario: Levels between the standard ones
- **WHEN** records arrive at Debug-4, Info+2, Error+4 and Error+12
- **THEN** their lines are labelled `DEBUG-4`, `INFO+2`, `ERROR+4` and `ERROR`, each on one line, and none `FATAL`

#### Scenario: Handler conformance
- **WHEN** the standard library's handler conformance tests run against it
- **THEN** they pass
