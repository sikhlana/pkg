## RENAMED Requirements

- FROM: `### Requirement: Middleware runs in the order given at registration`
- TO: `### Requirement: Middleware runs in the order it declares`

## MODIFIED Requirements

### Requirement: Middleware runs in the order it declares

Middleware SHALL run in ascending order, the lowest nearest the caller, so each sees what lower ones passed on. A middleware that declares an order SHALL use it, one wrapped with an order at wiring time SHALL use that order instead of its own, and any other SHALL count as order 0. Middleware with equal orders SHALL run in an unspecified relative order, and SHALL NOT fail startup. Middleware registered as a value and as a constructor SHALL be ordered together. A middleware that is nil, or a nil pointer, function, map, slice or channel, wrapped with an order or not, SHALL be left out, and records SHALL pass through the rest of the chain without it. The same ordering and the same leaving out SHALL apply to middleware passed to the logger's constructor outside fx.

#### Scenario: A higher order sees a lower one's work
- **WHEN** a middleware declaring order 10 adds an attribute and one declaring order 20 records what it receives
- **THEN** the order-20 middleware receives the record with that attribute

#### Scenario: A lower order drops
- **WHEN** a middleware declaring order 10 drops a record
- **THEN** the middleware declaring order 20 never sees it

#### Scenario: Values and constructors together
- **WHEN** a middleware registered as a constructor declares order 15 and middleware registered as values declare orders 10 and 20
- **THEN** they run in the order 10, 15, 20

#### Scenario: Undeclared order counts as zero
- **WHEN** a middleware that declares no order and one declaring order 5 each record that they ran
- **THEN** the undeclared one runs first

#### Scenario: An order set at wiring time
- **WHEN** a middleware declaring order 100 is wrapped with order 1, beside a middleware declaring order 50
- **THEN** the wrapped one runs first

#### Scenario: Equal orders
- **WHEN** two middleware both declare no order
- **THEN** the app starts, and every enabled record passes through both

#### Scenario: A constructor returns no middleware
- **WHEN** one middleware constructor returns nil because its feature is off, another returns a nil pointer, and a third returns a middleware that adds an attribute
- **THEN** the app starts, and every record is written with the third middleware's attribute

#### Scenario: Outside fx
- **WHEN** the logger is built without fx from a nil middleware and middleware declaring orders 20 and 10
- **THEN** records pass through the order-10 middleware, then the order-20 one, and the nil one is left out
