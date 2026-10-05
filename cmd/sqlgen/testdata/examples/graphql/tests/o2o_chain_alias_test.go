package tests

import (
	"fmt"
	"testing"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql/models"
	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/omittable"
)

// O2O alias regression coverage. Every O2O JOIN target gets its own alias, and
// the alias is written bare in SQL (PRD §13.2). The generator used to spell it
// as the shortest unused prefix of the edge name, so the `user_badges` ↔
// `users` loop named its fifth hop `user`, which PostgreSQL reserves: the
// query failed with `syntax error at or near "."` (SQLSTATE 42601). Aliases
// are now a letter plus a counter, which no dialect reserves.

// badgeChainOptions selects `hops` edges of the Users / Badge loop below a
// user_badges row: Users, Users.Badge, Users.Badge.Users, and so on.
func badgeChainOptions(hops int) *models.UserBadgeFieldOptions {
	root := &models.UserBadgeFieldOptions{ID: true, UserID: true}
	badge := root
	var user *models.UserFieldOptions
	for hop := 1; hop <= hops; hop++ {
		if hop%2 == 1 {
			user = &models.UserFieldOptions{ID: true}
			badge.Users = user
		} else {
			badge = &models.UserBadgeFieldOptions{ID: true, UserID: true}
			user.Badge = badge
		}
	}
	return root
}

// TestO2OChain_BadgeLoopAliasesAreNotKeywords walks the loop to the generator's
// depth cap (six hops) and asserts every hop resolves back to the linked pair.
func TestO2OChain_BadgeLoopAliasesAreNotKeywords(t *testing.T) {
	truncateAll(t)
	user, err := testClient.Users().Create(ctx(), &models.CreateUserInput{Email: "alias-chain@example.com", Name: "Alias Chain"})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	badge, err := testClient.UserBadges().Create(ctx(), &models.CreateUserBadgeInput{
		UserID: omittable.Set(&user.ID),
		Label:  "alias-chain",
	})
	if err != nil {
		t.Fatalf("create user badge: %v", err)
	}

	for hops := 1; hops <= 6; hops++ {
		t.Run(fmt.Sprintf("hops=%d", hops), func(t *testing.T) {
			got, err := testClient.UserBadges().GetMany(ctx(), &models.GetUserBadgesInput{
				Filter: &models.UserBadgeFilter{ID: &comparator.Number[int64]{Eq: new(badge.ID)}},
			}, func(o *models.CallOptions[models.UserBadgeFieldOptions]) {
				o.FieldOptions = badgeChainOptions(hops)
			})
			if err != nil {
				t.Fatalf("UserBadges().GetMany with a %d-hop Users/Badge chain: %v", hops, err)
			}
			if len(got) != 1 {
				t.Fatalf("UserBadges().GetMany returned %d rows, want 1", len(got))
			}
			b := got[0]
			for hop := 1; hop <= hops; hop++ {
				if hop%2 == 1 {
					if b.Users == nil {
						t.Fatalf("hop %d (Users) is nil, want user %s", hop, user.ID)
					}
					if b.Users.ID != user.ID {
						t.Errorf("hop %d (Users).ID = %s, want %s", hop, b.Users.ID, user.ID)
					}
					continue
				}
				if b.Users.Badge == nil {
					t.Fatalf("hop %d (Badge) is nil, want badge %d", hop, badge.ID)
				}
				b = b.Users.Badge
				if b.ID != badge.ID {
					t.Errorf("hop %d (Badge).ID = %d, want %d", hop, b.ID, badge.ID)
				}
			}
		})
	}
}
