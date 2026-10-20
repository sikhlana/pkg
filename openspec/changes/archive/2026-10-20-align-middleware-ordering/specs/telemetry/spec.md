## MODIFIED Requirements

### Requirement: Log records carry the trace and span ids

The log middleware SHALL add `trace_id` and `span_id`, as lowercase hex strings, to every record logged with a context carrying a valid span context, and SHALL pass every other record on unchanged. It SHALL declare order 500 in the log module's chain, so middleware ordered below it, such as samplers, run before it and middleware ordered above it, such as redactors, see its attributes.

#### Scenario: A record inside a span
- **WHEN** a record is logged with a context carrying a span with trace id T and span id S
- **THEN** the record has `trace_id` T and `span_id` S as hex strings

#### Scenario: A record without a span
- **WHEN** a record is logged with a context that carries no span context
- **THEN** the record has neither attribute

#### Scenario: Its place in the chain
- **WHEN** the log middleware is registered with the log module without an order, beside middleware declaring orders 400 and 600 that each record what they receive, and a record is logged inside a span
- **THEN** the order-400 middleware receives the record without `trace_id`, and the order-600 one receives it with `trace_id`
