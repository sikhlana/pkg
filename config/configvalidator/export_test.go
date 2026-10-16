package configvalidator

// Prefix exposes prefix to the external test package, with a key written as an operator would in place of a path, so
// key composition is tested without a Validator around it.
func Prefix(key string, err error) Errors {
	var at path

	if key != "" {
		at = path{{kind: rawKind, name: key}}
	}

	return prefix(at, err)
}
