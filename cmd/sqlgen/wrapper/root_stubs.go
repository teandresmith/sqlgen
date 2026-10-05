package wrapper

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"slices"
	"strings"
)

// UnboundRootFieldsError reports sqlgen-managed Query / Mutation fields that
// gqlgen left as `panic("not implemented")` stubs after the rewriter ran.
//
// gqlgen names every root resolver itself, with `templates.ToGo` over the
// GraphQL field name, and ignores a `fieldName` entry on a root field
// (`codegen/field.go` returns at `case obj.Root:` before reading it). Where
// its spelling differs from the method sqlgen's seed declares (`onlyPK` is
// `OnlyPk` to gqlgen and `OnlyPK` to sqlgen), gqlgen moves the seeded body into
// its `!!! WARNING !!!` block and emits a stub under its own name, which the
// rewriter does not recognise. The stub compiles and panics on the first
// request, so generation fails instead, naming the entity to rename.
type UnboundRootFieldsError struct {
	Fields []UnboundRootField
}

// UnboundRootField is one managed root field gqlgen left as a stub.
type UnboundRootField struct {
	// Owner is the entity the field belongs to. Zero when the managed set
	// carried no owner for it.
	Owner ManagedFieldOwner
	// Type is the root type, "Query" or "Mutation".
	Type string
	// GqlgenName is the resolver method gqlgen declared (e.g. "OnlyPk").
	GqlgenName string
	// SqlgenName is the resolver method sqlgen manages (e.g. "OnlyPK").
	SqlgenName string
	// File is the resolver file holding the stub.
	File string
}

func (e *UnboundRootFieldsError) Error() string {
	// Group by entity, in first-seen order, so each gets one remedy line.
	var order []string
	byOwner := make(map[string][]UnboundRootField)
	for _, f := range e.Fields {
		key := f.Owner.ConfigKey
		if _, seen := byOwner[key]; !seen {
			order = append(order, key)
		}
		byOwner[key] = append(byOwner[key], f)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "gqlgen named %d sqlgen-managed GraphQL resolver(s) differently from sqlgen and left them as panic(\"not implemented\") stubs; gqlgen names Query and Mutation resolvers itself and ignores fieldName on them (PRD §26.5.6)", len(e.Fields))
	for _, key := range order {
		fields := byOwner[key]
		parts := make([]string, 0, len(fields))
		files := make([]string, 0, 1)
		for _, f := range fields {
			field := f.Type
			if f.Owner.GraphQLName != "" {
				field += "." + f.Owner.GraphQLName
			}
			parts = append(parts, fmt.Sprintf("%s is %s to gqlgen and %s to sqlgen", field, f.GqlgenName, f.SqlgenName))
			if !slices.Contains(files, f.File) {
				files = append(files, f.File)
			}
		}
		fmt.Fprintf(&b, "\n  %s: %s (%s)", ownerLabel(key), strings.Join(parts, "; "), strings.Join(files, ", "))
		if key != "" {
			fmt.Fprintf(&b, "\n    set %s.struct_name to a name gqlgen spells the same way (it re-cases an acronym it does not know, and one that follows a digit), then regenerate", key)
		}
	}
	return b.String()
}

// ownerLabel renders "tables.only_pks" as `table "only_pks"`.
func ownerLabel(configKey string) string {
	kind, name, ok := strings.Cut(configKey, ".")
	switch {
	case !ok:
		return "unattributed field"
	case kind == "views":
		return fmt.Sprintf("view %q", name)
	default:
		return fmt.Sprintf("table %q", name)
	}
}

// settleManagedStubs runs once gqlgen has returned. It rewrites the stubs
// gqlgen emitted for managed fields into delegations, then fails if a managed
// field is still a stub. gqlgen names root resolvers itself and ignores
// fieldName on Query / Mutation fields, so a managed field it spells
// differently from sqlgen escapes the rewriter; the stub compiles and panics
// on the first request, so generation fails here instead.
func settleManagedStubs(opts GenOptions, layout resolverLayout) error {
	if err := rewritePanicStubs(opts, layout); err != nil {
		return fmt.Errorf("rewriting panic stubs: %w", err)
	}
	return checkManagedRootStubs(opts, layout)
}

// checkManagedRootStubs runs after rewritePanicStubs and fails when a
// sqlgen-managed root field is still a panic stub.
//
// A stub is attributed to a managed field when its method name equals the
// managed name ignoring case. gqlgen's ToGo only re-cases an alphanumeric
// GraphQL name, so that is exactly the drift this guards, and it needs no
// copy of gqlgen's naming rules. The stub's panic message is not used: under
// the single-file layout gqlgen writes a bare `panic("not implemented")`
// that carries no field name. An exact match is reported too, since one
// would mean the rewriter missed it.
//
// Consumer-authored fields are unaffected. Their stubs match no managed name,
// and leaving them as stubs is the consumer's business.
func checkManagedRootStubs(opts GenOptions, layout resolverLayout) error {
	managed := opts.SqlgenManagedFields
	if len(managed.QueryFields) == 0 && len(managed.MutationFields) == 0 {
		return nil
	}
	folded := make(map[string]string, len(managed.QueryFields)+len(managed.MutationFields))
	for _, f := range managed.QueryFields {
		folded["Q."+strings.ToLower(f)] = f
	}
	for _, f := range managed.MutationFields {
		folded["M."+strings.ToLower(f)] = f
	}

	files, err := gqlgenOwnedFiles(opts.WorkingDir, layout)
	if err != nil {
		return err
	}
	var unbound []UnboundRootField
	for _, path := range files {
		found, err := unboundStubsInFile(path, folded, managed.Owners)
		if err != nil {
			return err
		}
		unbound = append(unbound, found...)
	}
	if len(unbound) == 0 {
		return nil
	}
	return &UnboundRootFieldsError{Fields: unbound}
}

// unboundStubsInFile returns the panic stubs in one resolver file whose method
// is a managed root field. folded maps "Q." / "M." plus the lowered method
// name to sqlgen's spelling of it.
func unboundStubsInFile(path string, folded map[string]string, owners map[string]ManagedFieldOwner) ([]UnboundRootField, error) {
	src, err := os.ReadFile(path) //nolint:gosec // path comes from filepath.Glob under resolver dir
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), path, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	var out []UnboundRootField
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 || !isPanicStubBody(fn.Body) {
			continue
		}
		prefix, rootType := rootReceiver(receiverTypeName(fn.Recv.List[0]))
		if prefix == "" {
			continue
		}
		sqlgenName, ok := folded[prefix+strings.ToLower(fn.Name.Name)]
		if !ok {
			continue
		}
		out = append(out, UnboundRootField{
			Owner:      owners[prefix+sqlgenName],
			Type:       rootType,
			GqlgenName: fn.Name.Name,
			SqlgenName: sqlgenName,
			File:       path,
		})
	}
	return out, nil
}

// rootReceiver maps a resolver receiver to its managed-set key prefix and its
// root type name. Both are empty for any other receiver.
func rootReceiver(recvType string) (prefix, rootType string) {
	switch recvType {
	case "queryResolver":
		return "Q.", "Query"
	case "mutationResolver":
		return "M.", "Mutation"
	default:
		return "", ""
	}
}
