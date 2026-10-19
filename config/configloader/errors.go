package configloader

import "errors"

var (
	// ErrInvalidName is returned when Options.Name is empty, starts with a digit, or holds anything other than ASCII
	// letters, digits, - and _.
	ErrInvalidName = errors.New("invalid name")

	// ErrMalformedFile is returned when the config file can't be parsed. The error names the file, its format and, where
	// the parser reports one, the position, but never the parser's message, which can quote the file's contents.
	ErrMalformedFile = errors.New("malformed config file")

	// ErrEnvNameCollision is returned when two keys would share an env name, or one key's env name is another's
	// _FILE name. The error names both keys and the name.
	ErrEnvNameCollision = errors.New("env name collision")

	// ErrUnknownKeys is returned when the config file sets keys that match no field and Options.AllowUnknownKeys
	// isn't set. The error lists every such key in full, sorted.
	ErrUnknownKeys = errors.New("unknown config keys")

	// ErrEnvAndFileSet is returned when both a key's env variable and its _FILE variable are non-empty. The error
	// names both variables.
	ErrEnvAndFileSet = errors.New("env variable and its file variable both set")

	// ErrSecretFileTooLarge is returned when a _FILE variable names a file larger than 1 MiB. The error names the
	// variable and the path, never the contents.
	ErrSecretFileTooLarge = errors.New("secret file too large")
)
