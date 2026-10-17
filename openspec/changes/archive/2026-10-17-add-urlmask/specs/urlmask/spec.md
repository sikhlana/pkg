## Purpose

Turns a URL into a string that is safe to log, masking the parts that commonly carry secrets (query and path parameter values, passwords and fragments) while keeping enough of its shape to debug with.

## ADDED Requirements

### Requirement: Query values are masked

Every query value SHALL be replaced with `***`. Keys SHALL be kept as written in the URL, escaped form included, in their original order, and a repeated key SHALL appear once for each occurrence. An entry whose value contains `=` SHALL keep its key, with the whole value masked. A value that isn't validly escaped SHALL be masked like any other.

#### Scenario: Keys keep their order
- **WHEN** a URL with the query `token=abc&email=a@b.c` is masked
- **THEN** its query becomes `token=***&email=***`

#### Scenario: A repeated key keeps its count
- **WHEN** a URL with the query `a=1&b=2&a=3` is masked
- **THEN** its query becomes `a=***&b=***&a=***`

#### Scenario: Keys stay escaped as written
- **WHEN** a URL with the query `a%20b=1&c+d=2` is masked
- **THEN** its query becomes `a%20b=***&c+d=***`

#### Scenario: A value with an inner equals sign
- **WHEN** a URL with the query `sig=a=b` is masked
- **THEN** its query becomes `sig=***`

#### Scenario: A value that isn't validly escaped
- **WHEN** a URL with the query `token=%zz&lang=en` is masked
- **THEN** its query becomes `token=***&lang=***`

### Requirement: Bare entries are masked whole

A query entry with no `=`, or whose value is empty or made only of `=` characters, SHALL be replaced whole with `***`, key included, since it is often a token itself. It SHALL keep its position among the other entries. Empty entries SHALL be kept as they are.

#### Scenario: A bare token between keyed entries
- **WHEN** a URL with the query `lang=en&eyJhbGciOiJIUzI1NiJ9.payload.sig&page=2` is masked
- **THEN** its query becomes `lang=***&***&page=***`

#### Scenario: A padded base64 token
- **WHEN** a URL with the query `cmVzZXQ6dXNlcjQyOnMzY3IzdC1ub25jZSE=` is masked
- **THEN** its query becomes `***`

#### Scenario: An empty value
- **WHEN** a URL with the query `token=` is masked
- **THEN** its query becomes `***`

#### Scenario: Empty entries
- **WHEN** a URL with the query `a=1&&b=2` is masked
- **THEN** its query becomes `a=***&&b=***`

### Requirement: Ampersands and semicolons both separate query entries

Both `&` and `;` SHALL separate query entries, and each separator SHALL be kept as written.

#### Scenario: A token before a semicolon
- **WHEN** a URL with the query `s3cr3t;a=1` is masked
- **THEN** its query becomes `***;a=***`

#### Scenario: Mixed separators
- **WHEN** a URL with the query `a=1;b=2&c=3` is masked
- **THEN** its query becomes `a=***;b=***&c=***`

### Requirement: Path parameters are masked

In each path segment, everything after the first `;` SHALL be treated as `;`-separated parameters and masked by the same rules as query entries: a parameter's value becomes `***`, and a bare parameter becomes `***` whole. The part of each segment before the first `;` SHALL be kept as written, escaped form included. Only a literal `;` SHALL start path parameters: an escaped one (`%3B`) is data, and SHALL be passed through with the rest of its segment.

#### Scenario: A session ID in a path parameter
- **WHEN** a URL with the path `/app/cart;jsessionid=8F3A1C/items` is masked
- **THEN** its path becomes `/app/cart;jsessionid=***/items`

#### Scenario: Several parameters, one bare
- **WHEN** a URL with the path `/a;x=1;tok/b` is masked
- **THEN** its path becomes `/a;x=***;***/b`

#### Scenario: Escaped characters in the path
- **WHEN** a URL with the path `/a%20b/p%2Fq;k=v` is masked
- **THEN** its path becomes `/a%20b/p%2Fq;k=***`

#### Scenario: An escaped semicolon
- **WHEN** a URL with the path `/a%3Bjsessionid=8F3A1C` is masked
- **THEN** its path stays `/a%3Bjsessionid=8F3A1C`

### Requirement: Passwords are redacted and usernames kept

A password in the user information SHALL be replaced with `xxxxx`. The username SHALL be kept. User information with no password SHALL be kept as it is.

#### Scenario: A username and password
- **WHEN** `https://alice:hunter2@example.com/x` is masked
- **THEN** the result is `https://alice:xxxxx@example.com/x`

#### Scenario: A username alone
- **WHEN** `https://alice@example.com/x` is masked
- **THEN** the result is `https://alice@example.com/x`

### Requirement: The fragment is dropped

The fragment SHALL be removed, together with its `#`.

#### Scenario: An access token in the fragment
- **WHEN** `https://example.com/cb?state=1#access_token=SECRET` is masked
- **THEN** the result is `https://example.com/cb?state=***`

### Requirement: The rest of the URL is passed through

The host and port SHALL be kept as written, and the scheme SHALL come out lowercase. A URL with a lowercase scheme and no query, user information, fragment or path parameters SHALL come back unchanged.

#### Scenario: A URL with nothing to mask
- **WHEN** `https://example.com:8443/a/b` is masked
- **THEN** the result is `https://example.com:8443/a/b`

#### Scenario: An uppercase scheme
- **WHEN** `HTTPS://Example.com/x?t=1` is masked
- **THEN** the result is `https://Example.com/x?t=***`

#### Scenario: Everything at once
- **WHEN** `https://alice:pw@example.com:8443/a;sid=1/b?x=1&tok#frag` is masked
- **THEN** the result is `https://alice:xxxxx@example.com:8443/a;sid=***/b?x=***&***`

### Requirement: Unusual input is masked, not echoed

An opaque URL SHALL become its scheme followed by `:***`. A URL string that can't be parsed SHALL become `***`. A nil URL or an empty string SHALL become an empty string.

#### Scenario: An opaque URL
- **WHEN** `mailto:alice@example.com` is masked
- **THEN** the result is `mailto:***`

#### Scenario: Credentials in an opaque URL
- **WHEN** `https:alice:hunter2@example.com/x` is masked
- **THEN** the result is `https:***`

#### Scenario: An unparseable URL string
- **WHEN** `https://alice:hunter2@app example.com/?token=abc` is masked
- **THEN** the result is `***`

#### Scenario: No URL
- **WHEN** a nil URL or an empty string is masked
- **THEN** the result is an empty string

### Requirement: Every input form is masked alike

A URL string SHALL be masked to the same result as its parsed form, and a bare query SHALL be masked to the same result as that query inside a URL. Masking SHALL NOT modify the URL it is given.

#### Scenario: A string and its parsed form
- **WHEN** a URL string and the URL parsed from it are each masked
- **THEN** both results are equal

#### Scenario: A bare query
- **WHEN** the query `a=1;tok&b=2` is masked on its own
- **THEN** the result is `a=***;***&b=***`

#### Scenario: The caller's URL is untouched
- **WHEN** a URL with a password, a query and a fragment is masked
- **THEN** the URL the caller holds still has its password, query and fragment

### Requirement: A log value masks when the record is handled

The module SHALL provide a log value for a URL that resolves to the same string as masking that URL directly. The masking SHALL happen when a handler resolves the value, not when the value is created, so a record the logger drops costs no masking.

#### Scenario: A logged URL
- **WHEN** a record carrying the log value of `https://alice:pw@example.com/?t=1` is handled by a text handler
- **THEN** the record shows `https://alice:xxxxx@example.com/?t=***`

#### Scenario: A nil URL
- **WHEN** a record carrying the log value of a nil URL is handled
- **THEN** the record shows an empty string

### Requirement: Names and path segments aren't masked

Query keys, path parameter names, path segments outside their parameters (an escaped `;` included), usernames and hosts SHALL be logged as written. A secret placed in one of them is not masked, and consumers MUST NOT rely on the masking for them.

#### Scenario: A secret used as a query key
- **WHEN** a URL with the query `sk_live_abc=1` is masked
- **THEN** its query becomes `sk_live_abc=***`

#### Scenario: A token in a path segment
- **WHEN** `https://example.com/reset/8F3A1C` is masked
- **THEN** the result is `https://example.com/reset/8F3A1C`
