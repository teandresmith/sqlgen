package config

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// envRef is a scalar value that held a ${NAME} reference or a $${ escape
// (PRD §4.1.1).
type envRef struct {
	line int
	path string
	// text is the value as written in the file, which never holds a secret.
	text string
}

// expansion is what expandEnv did to a config tree.
type expansion struct {
	// refs lists every expanded scalar in document order.
	refs []envRef
	// vars maps a dotted path to the variables its value referenced. An alias
	// of an expanded scalar is listed under its own path too.
	vars map[string][]string
}

// referenceErrors lists every unset, invalid and unterminated reference in a
// config, in document order.
type referenceErrors []error

func (e referenceErrors) Error() string {
	var b strings.Builder
	b.WriteString("environment variable references:")
	for _, err := range e {
		b.WriteString("\n  ")
		b.WriteString(err.Error())
	}
	return b.String()
}

func (e referenceErrors) Unwrap() []error { return e }

// expandEnv replaces the ${NAME} references in every scalar value under root,
// in place. Keys are never expanded. With any unset, invalid or unterminated
// reference it returns a referenceErrors naming each one, and the tree is not
// fit to decode.
func expandEnv(root *yaml.Node) (expansion, error) {
	e := expander{
		byNode: make(map[*yaml.Node][]string),
		exp:    expansion{vars: make(map[string][]string)},
	}
	e.walk(root, "", true)
	if len(e.errs) > 0 {
		return expansion{}, e.errs
	}
	return e.exp, nil
}

type expander struct {
	exp    expansion
	errs   referenceErrors
	byNode map[*yaml.Node][]string
}

// walk visits every value under n. With expand false it is inside an alias,
// whose anchor was already expanded where it is defined, and only records the
// variables under the alias's path.
func (e *expander) walk(n *yaml.Node, path string, expand bool) {
	switch n.Kind {
	case yaml.DocumentNode:
		for _, c := range n.Content {
			e.walk(c, path, expand)
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			key := n.Content[i].Value
			if path != "" {
				key = path + "." + key
			}
			e.walk(n.Content[i+1], key, expand)
		}
	case yaml.SequenceNode:
		for i, c := range n.Content {
			e.walk(c, path+"["+strconv.Itoa(i)+"]", expand)
		}
	case yaml.AliasNode:
		e.walk(n.Alias, path, false)
	case yaml.ScalarNode:
		if expand {
			e.scalar(n, path)
		} else if vars := e.byNode[n]; len(vars) > 0 {
			e.exp.vars[path] = vars
		}
	}
}

func (e *expander) scalar(n *yaml.Node, path string) {
	if !strings.Contains(n.Value, "${") {
		return
	}
	// yaml.v3 quotes a value that fails an explicit tag in full, and the
	// strict decode of the raw text stops at the unexpanded one.
	if n.Style&yaml.TaggedStyle != 0 && n.Tag != "!!str" {
		e.errs = append(e.errs, fmt.Errorf("line %d: %s: a reference can't be in a %s-tagged value; leave it untagged, it is typed by its value", n.Line, path, n.Tag))
		return
	}
	value, vars, errs := expandValue(n.Value)
	for _, err := range errs {
		e.errs = append(e.errs, fmt.Errorf("line %d: %s: %w", n.Line, path, err))
	}
	if len(errs) > 0 {
		return
	}

	e.exp.refs = append(e.exp.refs, envRef{line: n.Line, path: path, text: n.Value})
	if len(vars) > 0 {
		e.exp.vars[path] = vars
		e.byNode[n] = vars
	}
	n.Value = value

	// A plain scalar is typed by its expanded text, as YAML types any plain
	// scalar, except never as null: a null decodes into a string field as "",
	// so a password that is literally "null" would vanish. Quoted, block and
	// explicitly tagged scalars keep their tag.
	const keepTag = yaml.TaggedStyle | yaml.DoubleQuotedStyle | yaml.SingleQuotedStyle | yaml.LiteralStyle | yaml.FoldedStyle
	if n.Style&keepTag == 0 {
		n.Tag = ""
		if n.ShortTag() == "!!null" {
			n.Tag = "!!str"
		}
	}
}

// expandValue substitutes every ${NAME} in s and turns each $${ into a literal
// ${. Any other $ is kept. Substituted text is not scanned again. It returns
// the variables referenced, in order, and one error per unset, invalid or
// unterminated reference.
func expandValue(s string) (string, []string, []error) {
	var (
		b    strings.Builder
		vars []string
		errs []error
	)
	for i := 0; i < len(s); {
		switch {
		case strings.HasPrefix(s[i:], "$${"):
			b.WriteString("${")
			i += 3
		case strings.HasPrefix(s[i:], "${"):
			end := strings.IndexByte(s[i:], '}')
			if end < 0 {
				errs = append(errs, fmt.Errorf("unterminated reference %q", s[i:]))
				return "", nil, errs
			}
			ref, name := s[i:i+end+1], s[i+2:i+end]
			i += end + 1
			if !validEnvName(name) {
				errs = append(errs, fmt.Errorf("invalid reference %q: a name is letters, digits and '_', not starting with a digit", ref))
				continue
			}
			value, ok := os.LookupEnv(name)
			if !ok {
				errs = append(errs, fmt.Errorf("environment variable %s is not set", name))
				continue
			}
			b.WriteString(value)
			if !slices.Contains(vars, name) {
				vars = append(vars, name)
			}
		default:
			b.WriteByte(s[i])
			i++
		}
	}
	if len(errs) > 0 {
		return "", nil, errs
	}
	return b.String(), vars, nil
}

func validEnvName(name string) bool {
	if name == "" || name[0] >= '0' && name[0] <= '9' {
		return false
	}
	for _, c := range []byte(name) {
		if c != '_' && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

// decodeExpanded decodes a config whose tree root holds expanded references
// into cfg. yaml.Node.Decode is never strict, so unknown keys come from a
// strict decode of the raw bytes instead: keys are never expanded, so that
// pass reports them correctly and at their real lines. Its other type errors
// are about unexpanded text and are dropped. Re-encoding the expanded tree for a
// strict decode would lose the line numbers.
func decodeExpanded(data []byte, root *yaml.Node, exp expansion, cfg *RootConfig) error {
	var msgs []string

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var raw RootConfig
	if err := dec.Decode(&raw); err != nil {
		terr, ok := errors.AsType[*yaml.TypeError](err)
		if !ok {
			// Any other error stops the decode before it has checked every
			// key. It quotes only the file's own text.
			return fmt.Errorf("decoding config: %w", err)
		}
		for _, msg := range terr.Errors {
			if strings.Contains(msg, " not found in type ") {
				msgs = append(msgs, msg)
			}
		}
	}

	err := root.Decode(cfg)
	terr, ok := errors.AsType[*yaml.TypeError](err)
	if err != nil && !ok {
		return fmt.Errorf("decoding expanded config: %w", err)
	}
	if ok {
		for _, msg := range terr.Errors {
			msgs = append(msgs, exp.redact(msg))
		}
	}
	if len(msgs) == 0 {
		return nil
	}
	slices.SortStableFunc(msgs, func(a, b string) int { return cmp.Compare(errorLine(a), errorLine(b)) })
	return &yaml.TypeError{Errors: msgs}
}

// redact rewrites a type error on a line that holds a reference to name the
// reference, because yaml.v3 quotes the offending value, which is the
// expanded text (PRD §4.1.1).
func (exp expansion) redact(msg string) string {
	_, typ, found := strings.CutLast(msg, " into ")
	// A sequence or mapping in the wrong place quotes no value, and the
	// culprit is that node, not a reference on its line.
	if !found || !strings.Contains(msg, ": cannot unmarshal ") ||
		strings.Contains(msg, "cannot unmarshal !!seq into") || strings.Contains(msg, "cannot unmarshal !!map into") {
		return msg
	}
	line := errorLine(msg)
	var paths, texts []string
	for _, r := range exp.refs {
		if r.line == line {
			paths = append(paths, r.path)
			texts = append(texts, strconv.Quote(r.text))
		}
	}
	if len(paths) == 0 {
		return msg
	}
	return fmt.Sprintf("line %d: %s: cannot unmarshal the expansion of %s into %s",
		line, strings.Join(paths, ", "), strings.Join(texts, ", "), typ)
}

// errorLine returns N from a yaml.v3 "line N: …" message, or 0.
func errorLine(msg string) int {
	rest, ok := strings.CutPrefix(msg, "line ")
	if !ok {
		return 0
	}
	n, _, _ := strings.Cut(rest, ":")
	line, _ := strconv.Atoi(n)
	return line
}
