package wrapper

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// onlyPKResolvers is the resolver file gqlgen v0.17.95 writes for an
// `only_pks` table under follow-schema, trimmed to the two managed fields:
// sqlgen's seed declared OnlyPK / DeleteOnlyPK, gqlgen ignored the root
// fieldName entries and named them OnlyPk / DeleteOnlyPk, and the seeded
// bodies went into its WARNING block (measured).
const onlyPKResolvers = `package graph

import (
	"context"
	"fmt"
)

// DeleteOnlyPk is the resolver for the deleteOnlyPK field.
func (r *mutationResolver) DeleteOnlyPk(ctx context.Context, id int64) (bool, error) {
	panic(fmt.Errorf("not implemented: DeleteOnlyPk - deleteOnlyPK"))
}

// OnlyPk is the resolver for the onlyPK field.
func (r *queryResolver) OnlyPk(ctx context.Context, id int64) (*int, error) {
	panic(fmt.Errorf("not implemented: OnlyPk - onlyPK"))
}

// !!! WARNING !!!
// The code below was going to be deleted when updating resolvers. It has been copied here so you have
// one last chance to move it out of harms way if you want. There are two reasons this happens:
//  - When renaming or deleting a resolver the old code will be put in here. You can safely delete
//    it when you're done.
//  - You have helper methods in this file. Move them out to keep these resolver files clean.
/*
	func (r *queryResolver) OnlyPK(ctx context.Context, id int64) (*int, error) {
	return r.Q.OnlyPK(ctx, id)
}
*/
`

func onlyPKManaged() SqlgenManagedFieldSet {
	return SqlgenManagedFieldSet{
		QueryFields:    []string{"OnlyPK"},
		MutationFields: []string{"DeleteOnlyPK"},
		Owners: map[string]ManagedFieldOwner{
			"Q.OnlyPK":       {ConfigKey: "tables.only_pks", GraphQLName: "onlyPK"},
			"M.DeleteOnlyPK": {ConfigKey: "tables.only_pks", GraphQLName: "deleteOnlyPK"},
		},
	}
}

// TestCheckManagedRootStubs pins the loud stub error. gqlgen names root
// resolvers itself and ignores fieldName on Query / Mutation fields, so a
// managed field it spells differently from sqlgen is left a panic stub the
// rewriter cannot see. That compiled and panicked on the first request; it
// now fails generation, naming the entity and its struct_name key.
func TestCheckManagedRootStubs(t *testing.T) {
	tests := []struct {
		name    string
		layout  resolverLayout
		file    string
		src     string
		managed SqlgenManagedFieldSet
		// want is the unbound fields, minus File; nil means no error.
		want []UnboundRootField
	}{
		{
			name:    "follow-schema stubs gqlgen re-cased",
			layout:  resolverLayout{Layout: layoutFollowSchema, Dir: "graph", FilenameTemplate: defaultFilenameTpl},
			file:    "only_pk_gen.resolvers.go",
			src:     onlyPKResolvers,
			managed: onlyPKManaged(),
			want: []UnboundRootField{
				{Owner: ManagedFieldOwner{ConfigKey: "tables.only_pks", GraphQLName: "deleteOnlyPK"}, Type: "Mutation", GqlgenName: "DeleteOnlyPk", SqlgenName: "DeleteOnlyPK"},
				{Owner: ManagedFieldOwner{ConfigKey: "tables.only_pks", GraphQLName: "onlyPK"}, Type: "Query", GqlgenName: "OnlyPk", SqlgenName: "OnlyPK"},
			},
		},
		{
			// The single-file stub carries no field name, so the check
			// cannot rely on the panic message.
			name:   "single-file bare stub",
			layout: resolverLayout{Layout: layoutSingleFile, Dir: "graph", Filename: "graph/" + defaultSingleFilename},
			file:   defaultSingleFilename,
			src: `package graph

import "context"

func (r *queryResolver) GpuJob(ctx context.Context, id int64) (*int, error) {
	panic("not implemented")
}
`,
			managed: SqlgenManagedFieldSet{
				QueryFields: []string{"GPUJob"},
				Owners:      map[string]ManagedFieldOwner{"Q.GPUJob": {ConfigKey: "views.gpu_jobs", GraphQLName: "gpuJob"}},
			},
			want: []UnboundRootField{
				{Owner: ManagedFieldOwner{ConfigKey: "views.gpu_jobs", GraphQLName: "gpuJob"}, Type: "Query", GqlgenName: "GpuJob", SqlgenName: "GPUJob"},
			},
		},
		{
			name:   "consumer stub and implemented managed fields pass",
			layout: resolverLayout{Layout: layoutFollowSchema, Dir: "graph", FilenameTemplate: defaultFilenameTpl},
			file:   "product_gen.resolvers.go",
			src: `package graph

import (
	"context"
	"fmt"
)

func (r *queryResolver) Product(ctx context.Context, id int64) (*int, error) {
	return r.Q.Product(ctx, id)
}

func (r *queryResolver) TopSellers(ctx context.Context) ([]int, error) {
	panic(fmt.Errorf("not implemented: TopSellers - topSellers"))
}

func (r *productResolver) Product(ctx context.Context) (int, error) {
	panic("not implemented")
}
`,
			managed: SqlgenManagedFieldSet{QueryFields: []string{"Product"}},
		},
		{
			// A query and a mutation named alike are matched per receiver.
			name:   "receiver scopes the match",
			layout: resolverLayout{Layout: layoutFollowSchema, Dir: "graph", FilenameTemplate: defaultFilenameTpl},
			file:   "only_pk_gen.resolvers.go",
			src: `package graph

import "context"

func (r *mutationResolver) OnlyPk(ctx context.Context) (bool, error) {
	panic("not implemented")
}
`,
			managed: SqlgenManagedFieldSet{QueryFields: []string{"OnlyPK"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.MkdirAll(filepath.Join(dir, "graph"), 0o750); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			path := filepath.Join(dir, "graph", tt.file)
			if err := os.WriteFile(path, []byte(tt.src), 0o600); err != nil {
				t.Fatalf("writing fixture: %v", err)
			}

			err := checkManagedRootStubs(GenOptions{WorkingDir: dir, SqlgenManagedFields: tt.managed}, tt.layout)
			if tt.want == nil {
				if err != nil {
					t.Fatalf("checkManagedRootStubs: unexpected error: %v", err)
				}
				return
			}
			ue, ok := errors.AsType[*UnboundRootFieldsError](err)
			if !ok {
				t.Fatalf("checkManagedRootStubs = %v, want an *UnboundRootFieldsError", err)
			}
			got := make([]UnboundRootField, 0, len(ue.Fields))
			for _, f := range ue.Fields {
				if f.File != path {
					t.Errorf("%s.%s File = %q, want %q", f.Type, f.GqlgenName, f.File, path)
				}
				f.File = ""
				got = append(got, f)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("unbound fields (-want +got):\n%s", diff)
			}
		})
	}
}

// TestUnboundRootFieldsError_namesEntityAndRemedy pins the message the user
// acts on: the entity, both spellings of each resolver, the file, and the
// struct_name key that renames the entity.
func TestUnboundRootFieldsError_namesEntityAndRemedy(t *testing.T) {
	err := &UnboundRootFieldsError{Fields: []UnboundRootField{
		{Owner: ManagedFieldOwner{ConfigKey: "tables.only_pks", GraphQLName: "deleteOnlyPK"}, Type: "Mutation", GqlgenName: "DeleteOnlyPk", SqlgenName: "DeleteOnlyPK", File: "graph/only_pk_gen.resolvers.go"},
		{Owner: ManagedFieldOwner{ConfigKey: "tables.only_pks", GraphQLName: "onlyPK"}, Type: "Query", GqlgenName: "OnlyPk", SqlgenName: "OnlyPK", File: "graph/only_pk_gen.resolvers.go"},
		{Owner: ManagedFieldOwner{ConfigKey: "views.gpu_stats", GraphQLName: "gpuStat"}, Type: "Query", GqlgenName: "GpuStat", SqlgenName: "GPUStat", File: "graph/gpu_stat_gen.resolvers.go"},
	}}
	msg := err.Error()
	for _, want := range []string{
		`table "only_pks": Mutation.deleteOnlyPK is DeleteOnlyPk to gqlgen and DeleteOnlyPK to sqlgen; Query.onlyPK is OnlyPk to gqlgen and OnlyPK to sqlgen (graph/only_pk_gen.resolvers.go)`,
		"set tables.only_pks.struct_name to a name gqlgen spells the same way",
		`view "gpu_stats": Query.gpuStat is GpuStat to gqlgen and GPUStat to sqlgen (graph/gpu_stat_gen.resolvers.go)`,
		"set views.gpu_stats.struct_name to a name gqlgen spells the same way",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error is missing %q:\n%s", want, msg)
		}
	}
	if n := strings.Count(msg, "struct_name"); n != 2 {
		t.Errorf("error states the remedy %d times, want once per entity (2):\n%s", n, msg)
	}
}

// TestGen_failsOnManagedRootStub pins the check's place in Gen: after the
// gqlgen subprocess and the rewriter, before Gen reports success. The stub
// helper exits 0 without touching the resolver dir, which stands in for a
// gqlgen run that left the re-cased stubs in place.
func TestGen_failsOnManagedRootStub(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test helper invocation pattern is POSIX-only")
	}
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "gqlgen.yml")
	if err := os.WriteFile(cfgPath, []byte("schema:\n  - graph/*.graphqls\nresolver:\n  layout: follow-schema\n  dir: graph\n"), 0o600); err != nil {
		t.Fatalf("seeding gqlgen.yml: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "graph"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "graph", "only_pk_gen.resolvers.go"), []byte(onlyPKResolvers), 0o600); err != nil {
		t.Fatalf("writing resolver file: %v", err)
	}
	isolateTempDir(t)
	t.Setenv("SQLGEN_WRAPPER_TEST_HELPER", "succeed")

	_, err := Gen(context.Background(), GenOptions{
		GqlgenConfigPath:    cfgPath,
		GqlgenBin:           os.Args[0],
		MergeInput:          MergeInput{SchemaGlob: "graph/*.graphqls"},
		WorkingDir:          dir,
		SqlgenManagedFields: onlyPKManaged(),
		SeedPackage:         "graph",
		Stdout:              &bytes.Buffer{},
		Stderr:              &bytes.Buffer{},
	})
	if _, ok := errors.AsType[*UnboundRootFieldsError](err); !ok {
		t.Fatalf("Gen = %v, want an *UnboundRootFieldsError for the re-cased only_pks resolvers", err)
	}
}
