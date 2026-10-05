package tests

import (
	"context"
	"testing"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/mysql/models"
	"github.com/teandresmith/sqlgen/comparator"
)

// A JSON filter and an O2O join on the SAME table.
//
// `sql.PrefixConditions` alias-qualifies every filter condition once a join is
// active. It used to do that by prepending `alias + "."` to the whole clause,
// which is correct only for a clause that LEADS with its column. MySQL's JSON
// arms lead with a function name, so `JSON_CONTAINS(metadata, ?)` became
// `u.JSON_CONTAINS(metadata, ?)` — which MySQL resolves as a stored routine in
// schema `u`, failing at request time with
//
//	Error 1370 (42000): execute command denied to user 'test'@'%'
//	                    for routine 'u.JSON_CONTAINS'
//
// (or `ERROR 1305: FUNCTION u.JSON_CONTAINS does not exist` for a user holding
// broader grants — either way the message points at privileges rather than at
// the real cause).
//
// `users.metadata` is JSON and `users` has an O2O to `profiles`, so this is
// reachable in this example with no schema change. Dropping the qualification
// instead of fixing it is NOT an alternative: unqualified `metadata` is
// ambiguous whenever a joined table carries the same column name, which is why
// the condition now names its column and the alias lands on the argument.
func TestFilter_JSONUnderO2OJoin_MySQL(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	v := any(`{"tier":"gold"}`)
	users, err := client.Users().GetMany(
		ctx,
		&models.GetUsersInput{
			Filter: &models.UserFilter{
				Metadata: &comparator.NullableJSON{
					JSON: comparator.JSON{Contains: &v},
				},
			},
		},
		func(o *models.CallOptions[models.UserFieldOptions]) {
			o.FieldOptions = &models.UserFieldOptions{
				ID:      true,
				Profile: &models.ProfileFieldOptions{ID: true},
			}
		},
	)
	if err != nil {
		t.Fatalf("JSON contains under O2O join: %v", err)
	}
	// The query running at all is the regression guard; no row need match.
	t.Logf("matched %d users", len(users))

	// HasKey is the second arm and takes the same path.
	key := "$.tier"
	if _, err := client.Users().GetMany(
		ctx,
		&models.GetUsersInput{
			Filter: &models.UserFilter{
				Metadata: &comparator.NullableJSON{
					JSON: comparator.JSON{HasKey: &key},
				},
			},
		},
		func(o *models.CallOptions[models.UserFieldOptions]) {
			o.FieldOptions = &models.UserFieldOptions{
				ID:      true,
				Profile: &models.ProfileFieldOptions{ID: true},
			}
		},
	); err != nil {
		t.Fatalf("JSON hasKey under O2O join: %v", err)
	}
}
