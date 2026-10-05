package tests

import (
	"slices"
	"testing"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql/models"
)

// `user_sessions` is `api.enabled: false`, and `users` carries an FK-inferred
// edge into it (PRD §26.10, §9.9.4). The edge once stayed on `type User` as
// `userSessions: [UserSession!]!`, and gqlgen refused to load a schema that
// named a type it never declared, so no test in this package got past TestMain.
// The two tests below pin what the booted schema declares instead, and that the
// Go client still has the edge.

// TestAPIHiddenTarget_SchemaNamesItNowhere asks the running schema, by
// introspection, for every place the edge could surface: the hidden type
// itself, the parent's object type, and the three nested wrappers.
func TestAPIHiddenTarget_SchemaNamesItNowhere(t *testing.T) {
	type named struct {
		Name string `json:"name"`
	}
	// An object type answers `fields`, an input type `inputFields`; each query
	// below asks for the one its type has, so the other stays empty.
	type introspected struct {
		Fields      []named `json:"fields"`
		InputFields []named `json:"inputFields"`
	}
	var out struct {
		Session *named        `json:"session"`
		User    *introspected `json:"user"`
		Create  *introspected `json:"create"`
		Update  *introspected `json:"update"`
		Upsert  *introspected `json:"upsert"`
	}
	gqlExecData(t, `
		query {
			session: __type(name: "UserSession") { name }
			user: __type(name: "User") { fields { name } }
			create: __type(name: "CreateUserWithRelatedInput") { inputFields { name } }
			update: __type(name: "UpdateUserWithRelatedInput") { inputFields { name } }
			upsert: __type(name: "UpsertUserWithRelatedInput") { inputFields { name } }
		}
	`, nil, &out)

	if out.Session != nil {
		t.Errorf("__type(UserSession) = %+v, want null — the table is api.enabled: false", *out.Session)
	}

	tests := []struct {
		typeName string
		typ      *introspected
	}{
		{"User", out.User},
		{"CreateUserWithRelatedInput", out.Create},
		{"UpdateUserWithRelatedInput", out.Update},
		{"UpsertUserWithRelatedInput", out.Upsert},
	}
	for _, tt := range tests {
		t.Run(tt.typeName, func(t *testing.T) {
			if tt.typ == nil {
				t.Fatalf("__type(%s) = null, want the type", tt.typeName)
			}
			var got []string
			for _, f := range append(tt.typ.Fields, tt.typ.InputFields...) {
				got = append(got, f.Name)
			}
			if slices.Contains(got, "userSessions") {
				t.Errorf("%s declares userSessions; an edge into an api-disabled table must not reach the API: %v", tt.typeName, got)
			}
			// The control: `events` is on the API, so its edge is on every one
			// of these types. Without it, a wrong type name would empty the
			// list and pass the absence check above vacuously.
			if !slices.Contains(got, "events") {
				t.Errorf("%s has no events member; want the exposed edge kept: %v", tt.typeName, got)
			}
		})
	}
}

// TestAPIHiddenTarget_GoClientKeepsTheEdge is the other half: `api` gates the
// API, never the client. The nested write and the relationship read both go
// through the edge the schema no longer declares.
func TestAPIHiddenTarget_GoClientKeepsTheEdge(t *testing.T) {
	created, err := testClient.Users().CreateWithRelated(ctx(), &models.CreateUserWithRelatedInput{
		User: models.CreateUserInput{Email: "hidden-target@example.com", Name: "Hidden Target"},
		UserSessions: &models.UserUserSessionsCreateNested{
			Create: []*models.UserUserSessionsCreateInput{{TokenHash: "a"}, {TokenHash: "b"}},
		},
	})
	if err != nil {
		t.Fatalf("CreateWithRelated with UserSessions: %v", err)
	}

	got, err := testClient.Users().Get(ctx(), created.ID, func(o *models.CallOptions[models.UserFieldOptions]) {
		o.FieldOptions = &models.UserFieldOptions{
			ID: true,
			UserSessions: &models.UserSessionRelationshipOptions{
				FieldOptions: &models.UserSessionFieldOptions{ID: true, TokenHash: true},
			},
		}
	})
	if err != nil {
		t.Fatalf("Get user with UserSessions: %v", err)
	}
	tokens := make([]string, 0, len(got.UserSessions))
	for _, s := range got.UserSessions {
		tokens = append(tokens, s.TokenHash)
	}
	slices.Sort(tokens)
	if want := []string{"a", "b"}; !slices.Equal(tokens, want) {
		t.Errorf("User.UserSessions tokens = %v, want %v", tokens, want)
	}
}
