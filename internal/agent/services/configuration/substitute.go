package configuration

// substitute.go resolves ${env:VAR}, ${env:VAR:-default}, ${file:/path}
// and ${file:/path:-default} references inside the string values of a
// loaded configuration. The literal `$$` escapes to a single `$` so
// operators can embed a real dollar sign in a value when the leading
// `${` would otherwise trigger substitution.
//
// Substitution applies to string VALUES only — never to YAML keys.
// The walker descends into structs, pointers, maps and slices via
// reflection, mutating every settable string field in place.
//
// Errors short-circuit: the first unresolved reference (missing file
// with no default, malformed expression) aborts the walk and surfaces
// the offending reference. Empty environment variables WITHOUT a
// default substitute to an empty string and are NOT an error — this
// preserves the long-standing shell convention where `${UNSET}`
// expands to "" and matches what users expect when they write
// `${env:OPTIONAL_FEATURE_FLAG}`.

import (
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strings"
)

// dollarSentinel replaces `$$` during the pre-pass so the substitution
// regex cannot see it. The NUL byte is illegal in YAML scalars, so it
// can never appear in a real user value and is therefore a safe
// placeholder. Restored to `$` at the end of substituteString.
const dollarSentinel = "\x00DOLLAR\x00"

// substitutionPattern captures the three pieces of a reference:
//   - kind  = "env" or "file"
//   - ref   = the variable name or file path (non-greedy so the
//     optional `:-default` boundary is found first)
//   - dflt  = the default expression after `:-`, if any (empty means
//     no default was supplied)
//
// `[^}]+?` is non-greedy on purpose: it lets the optional `:-default`
// branch claim the suffix when present, while a Windows-style file
// path like `${file:C:/etc/x.key}` still parses correctly because the
// regex backtracks until the closing `}` matches.
var substitutionPattern = regexp.MustCompile(`\$\{(env|file):([^}]+?)(?::-([^}]*))?\}`)

// Substitute walks v recursively and resolves every ${env:...} and
// ${file:...} reference found in string fields. v must be a pointer
// (or contain pointers) for the mutations to be visible to the
// caller — calling Substitute(myStruct) with a non-pointer is a
// silent no-op for the top level, hence the explicit check below.
//
// Returns the first error encountered (unresolved reference, file
// missing without a default, regex match with no kind matched —
// the last is impossible by construction but reported defensively).
// On error, fields already mutated keep their substituted values;
// callers should treat a non-nil error as "configuration broken,
// abort boot" rather than trying to recover partially.
func Substitute(v interface{}) error {
	if v == nil {
		return nil
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Ptr {
		return fmt.Errorf("Substitute requires a pointer; got %T", v)
	}
	return walk(rv.Elem())
}

// walk dispatches by reflect.Kind, recursing into composite types
// and mutating settable string scalars in place. For non-addressable
// positions (map values, interface boxes) we substitute via a fresh
// reflect.Value and write back through the parent — the indirection
// is necessary because reflect cannot SetString on a value that
// doesn't have a home in memory.
func walk(v reflect.Value) error {
	switch v.Kind() {
	case reflect.Ptr:
		if v.IsNil() {
			return nil
		}
		return walk(v.Elem())

	case reflect.Interface:
		if v.IsNil() {
			return nil
		}
		// The interface is a box. We can't mutate the box's contents
		// in place, but if the caller (struct field, slice element)
		// has the interface addressable, we can rebuild a new value
		// and Set() it back. substituteInterface handles the round
		// trip: read concrete → mutate → repack → write back if v is
		// settable.
		return substituteInterface(v)

	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if !v.Field(i).CanSet() {
				// Unexported field — yaml decoders cannot populate
				// it, so any reference inside is synthetic and
				// skipping is the safe call.
				continue
			}
			if err := walk(v.Field(i)); err != nil {
				return err
			}
		}

	case reflect.Map:
		// Map values are not addressable. We extract the value,
		// reflect-copy it into a settable temporary, walk the
		// temporary, then SetMapIndex with whatever the walker
		// produced. Spec rule 2: keys are NEVER mutated.
		iter := v.MapRange()
		for iter.Next() {
			val := iter.Value()
			tmp := reflect.New(val.Type()).Elem()
			tmp.Set(val)
			if err := walk(tmp); err != nil {
				return err
			}
			v.SetMapIndex(iter.Key(), tmp)
		}

	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if err := walk(v.Index(i)); err != nil {
				return err
			}
		}

	case reflect.String:
		if !v.CanSet() {
			// Reachable only via a malformed reflection path; report
			// rather than silently dropping the substitution.
			return fmt.Errorf("string at %s is not settable; pass a pointer to Substitute", v.Type())
		}
		out, err := substituteString(v.String())
		if err != nil {
			return err
		}
		v.SetString(out)
	}
	return nil
}

// substituteInterface handles the awkward case of a reflect.Value
// whose Kind is Interface — the value points to a concrete payload
// (often string or map) but reflect.Value.Elem() on an interface
// yields a non-addressable view that cannot be mutated directly.
//
// The trick: build a fresh, addressable holder of the underlying
// type, copy the payload in, recurse, and write the (now mutated)
// holder back to the original interface via Set(). v itself must
// be settable for the write-back; non-settable interface positions
// (e.g. the top of a non-pointer call) are a programmer error.
func substituteInterface(v reflect.Value) error {
	concrete := v.Elem()
	holder := reflect.New(concrete.Type()).Elem()
	holder.Set(concrete)
	if err := walk(holder); err != nil {
		return err
	}
	if v.CanSet() {
		v.Set(holder)
	}
	return nil
}

// substituteString is the value-level resolver. Exported via package
// alias for unit testing (substituteString helper variant in _test).
// Returns the substituted string and the first error encountered.
//
// The pre/post-pass for `$$` exists so that an operator writing
// `password: "literal $$ sign"` ends up with `literal $ sign` in
// the final config — without the sentinel the regex would not
// match anything (no `{`) and we'd leave `$$` in place; the spec
// requires the escape, so the sentinel pass is non-optional.
func substituteString(s string) (string, error) {
	if !strings.Contains(s, "${") && !strings.Contains(s, "$$") {
		return s, nil // fast path — no references, no escape
	}

	// 1. Escape literal `$$` → sentinel so the regex below cannot
	//    accidentally consume it.
	escaped := strings.ReplaceAll(s, "$$", dollarSentinel)

	// 2. Substitute every ${env:...} and ${file:...} reference.
	var firstErr error
	result := substitutionPattern.ReplaceAllStringFunc(escaped, func(match string) string {
		if firstErr != nil {
			return match
		}
		groups := substitutionPattern.FindStringSubmatch(match)
		// groups[0] = full match, [1] = kind, [2] = ref, [3] = default
		kind, ref, dflt := groups[1], groups[2], groups[3]
		hasDefault := strings.Contains(match, ":-")

		value, err := resolveReference(kind, ref, dflt, hasDefault)
		if err != nil {
			firstErr = err
			return match
		}
		return value
	})
	if firstErr != nil {
		return "", firstErr
	}

	// 3. Restore the sentinel to a single `$` — never to `$$` so the
	//    operator's intent ("emit a literal dollar") is honoured.
	result = strings.ReplaceAll(result, dollarSentinel, "$")
	return result, nil
}

// resolveReference returns the resolved value for a single
// `${env:..}` or `${file:..}` match. hasDefault is passed alongside
// dflt because an explicit empty default (`${env:X:-}`) is different
// from "no default specified" (`${env:X}`) — only the latter is
// allowed to resolve to "" when env is unset.
func resolveReference(kind, ref, dflt string, hasDefault bool) (string, error) {
	switch kind {
	case "env":
		if val, ok := os.LookupEnv(ref); ok {
			return val, nil
		}
		// Missing env var: explicit default wins, otherwise empty
		// string. This matches POSIX shell `${X-}` semantics and is
		// the convention every operator who has ever written a
		// `docker-compose.yml` already knows.
		if hasDefault {
			return dflt, nil
		}
		return "", nil

	case "file":
		data, err := os.ReadFile(ref) // #nosec G304 - file path comes from the operator's config; this is the documented mechanism
		if err != nil {
			if hasDefault {
				return dflt, nil
			}
			return "", fmt.Errorf("reading ${file:%s}: %w", ref, err)
		}
		// Trim trailing whitespace/newlines — operators routinely
		// store secrets with a trailing newline from `echo "secret" > file`
		// and that newline would corrupt anything appended to the
		// resolved value (URLs, headers, DSNs).
		return strings.TrimSpace(string(data)), nil

	default:
		// Unreachable by construction (regex restricts kind to
		// env|file), but defensively reported instead of silently
		// emitting the literal match — silent skips are the design
		// failure this package exists to avoid.
		return "", fmt.Errorf("unknown substitution kind %q in ${%s:%s}", kind, kind, ref)
	}
}
