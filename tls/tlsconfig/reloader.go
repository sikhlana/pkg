package tlsconfig

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

// format says how a reloader turns its files into a value and reports it.
type format[T any] struct {
	// parse turns the files' contents into a value.
	parse func(contents [][]byte) (*T, error)

	// describe returns attributes naming a value in log records, or none. Never anything secret.
	describe func(v *T) []slog.Attr

	// check rejects a refreshed value that parses but mustn't replace the current one, or is nil to accept any. A
	// first load isn't checked: failing a start on it would help nobody.
	check func(v *T) error
}

// keyPairFormat reads a certificate file and a key file as a key pair. A refreshed pair is rejected outside its
// leaf's validity period, so a broken renewal can't replace a pair that still works.
var keyPairFormat = format[tls.Certificate]{parse: parseKeyPair, describe: describeKeyPair, check: checkKeyPair}

// poolFormat reads one file of PEM certificates as a pool.
var poolFormat = format[x509.CertPool]{parse: parsePool}

// reloader holds a value parsed from one or more files, and refreshes it during handshakes once the interval has
// passed since the files were last checked. Every method is safe for concurrent use.
type reloader[T any] struct {
	name     string
	paths    []string
	attrs    []slog.Attr
	format   format[T]
	interval time.Duration
	logger   *slog.Logger
	start    time.Time

	current atomic.Pointer[T]

	// lastCheck is the time of the last check, as the time since start, so it keeps start's monotonic reading.
	lastCheck atomic.Int64

	// mu guards seen and serialises checks. A handshake that can't take it skips the check rather than waiting.
	mu sync.Mutex

	// seen holds a digest of each file's last contents rather than the contents, so a private key isn't kept twice.
	seen [][sha256.Size]byte
}

// newReloader reads and parses the files, returning the parse or read error, and logs the value at Info. name is the
// value's name in log records, attrs name the files, and logger, which must not be nil, receives the records.
func newReloader[T any](
	name string,
	paths []string,
	attrs []slog.Attr,
	f format[T],
	interval time.Duration,
	logger *slog.Logger,
) (*reloader[T], error) {
	contents, err := readFiles(paths)
	if err != nil {
		return nil, err
	}

	v, err := f.parse(contents)
	if err != nil {
		return nil, err
	}

	r := &reloader[T]{
		name:     name,
		paths:    paths,
		attrs:    attrs,
		format:   f,
		interval: interval,
		logger:   logger,
		start:    time.Now(),
		seen:     digests(contents),
	}

	r.current.Store(v)
	r.log(context.Background(), slog.LevelInfo, name+" loaded", v, nil)

	return r, nil
}

// get returns the current value, first refreshing it when the interval has passed and no other handshake is already
// checking.
func (r *reloader[T]) get(ctx context.Context) *T {
	if r.due() && r.mu.TryLock() {
		defer r.mu.Unlock()

		// Another handshake may have checked between due and TryLock.
		if r.due() {
			r.refresh(ctx)
		}
	}

	return r.current.Load()
}

func (r *reloader[T]) due() bool {
	return time.Since(r.start)-time.Duration(r.lastCheck.Load()) >= r.interval
}

// refresh re-reads the files and swaps in their parsed contents when they've changed. On failure it keeps the current
// value. Contents that fail to parse or the format's check are logged once, since they're remembered as seen; a read
// failure has no contents to remember, so it's logged at every check. Callers hold mu.
func (r *reloader[T]) refresh(ctx context.Context) {
	r.lastCheck.Store(int64(time.Since(r.start)))

	contents, err := readFiles(r.paths)
	if err != nil {
		r.log(ctx, slog.LevelWarn, "reading "+r.name+" failed, keeping the last good one", nil, err)

		return
	}

	seen := digests(contents)

	if slices.Equal(seen, r.seen) {
		return
	}

	r.seen = seen

	v, err := r.format.parse(contents)
	if err != nil {
		r.log(ctx, slog.LevelWarn, "parsing "+r.name+" failed, keeping the last good one", nil, err)

		return
	}

	if r.format.check != nil {
		if checkErr := r.format.check(v); checkErr != nil {
			r.log(ctx, slog.LevelWarn, "rejecting "+r.name+", keeping the last good one", v, checkErr)

			return
		}
	}

	r.current.Store(v)
	r.log(ctx, slog.LevelInfo, r.name+" reloaded", v, nil)
}

// log writes a record naming the files, then v, when it's given and the format describes values, then err.
func (r *reloader[T]) log(ctx context.Context, level slog.Level, msg string, v *T, err error) {
	attrs := slices.Clip(r.attrs)

	if v != nil && r.format.describe != nil {
		attrs = append(attrs, r.format.describe(v)...)
	}

	if err != nil {
		attrs = append(attrs, slog.Any("error", err))
	}

	r.logger.LogAttrs(ctx, level, msg, attrs...)
}

// readFiles reads each path in turn, returning the first error, which names its path.
func readFiles(paths []string) ([][]byte, error) {
	contents := make([][]byte, 0, len(paths))

	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}

		contents = append(contents, data)
	}

	return contents, nil
}

// digests returns the SHA-256 digest of each file's contents.
func digests(contents [][]byte) [][sha256.Size]byte {
	sums := make([][sha256.Size]byte, len(contents))

	for i, data := range contents {
		sums[i] = sha256.Sum256(data)
	}

	return sums
}

// parseKeyPair parses a certificate file and its key file, with the parsed leaf in Leaf.
func parseKeyPair(contents [][]byte) (*tls.Certificate, error) {
	pair, err := tls.X509KeyPair(contents[0], contents[1])
	if err != nil {
		return nil, err
	}

	return &pair, nil
}

// describeKeyPair names a key pair by its leaf.
func describeKeyPair(pair *tls.Certificate) []slog.Attr {
	return []slog.Attr{
		slog.String("subject", pair.Leaf.Subject.String()),
		slog.String("serial", pair.Leaf.SerialNumber.String()),
		slog.Time("not_after", pair.Leaf.NotAfter),
	}
}

// checkKeyPair rejects a key pair whose leaf has expired or isn't valid yet.
func checkKeyPair(pair *tls.Certificate) error {
	now := time.Now()

	switch {
	case now.After(pair.Leaf.NotAfter):
		return fmt.Errorf("%w: expired at %s", ErrCertificateNotCurrent, pair.Leaf.NotAfter.Format(time.RFC3339))
	case now.Before(pair.Leaf.NotBefore):
		return fmt.Errorf("%w: not valid until %s", ErrCertificateNotCurrent, pair.Leaf.NotBefore.Format(time.RFC3339))
	default:
		return nil
	}
}

// parsePool parses a file of PEM certificates into a pool, failing with ErrNoCertificates when it holds none.
func parsePool(contents [][]byte) (*x509.CertPool, error) {
	pool := x509.NewCertPool()

	if !pool.AppendCertsFromPEM(contents[0]) {
		return nil, ErrNoCertificates
	}

	return pool, nil
}
