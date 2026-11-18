package wrapper

import (
	"testing"
)

// upfOmittableAt returns the `omittable` value at
// models.<typeName>.fields.<field>, and whether the key is present at all.
func upfOmittableAt(t *testing.T, merged []byte, typeName, field string) (value any, present bool) {
	t.Helper()
	models, ok := unmarshalMap(t, merged)["models"].(map[string]any)
	if !ok {
		return nil, false
	}
	entry, ok := models[typeName].(map[string]any)
	if !ok {
		return nil, false
	}
	fields, ok := entry["fields"].(map[string]any)
	if !ok {
		return nil, false
	}
	f, ok := fields[field].(map[string]any)
	if !ok {
		return nil, false
	}
	value, present = f["omittable"]
	return value, present
}

// TestMerge_ModelFieldsWritesOmittable pins that a field sqlgen marks
// omittable reaches gqlgen as a boolean `omittable: true` on the field's own
// entry, alongside its fieldName, and that an unmarked field gets no key.
func TestMerge_ModelFieldsWritesOmittable(t *testing.T) {
	merged, err := Merge(nil, MergeInput{
		ModelFields: map[string]map[string]ModelField{
			"UpdateEventInput": {
				"retryCount": {FieldName: "RetryCount", Omittable: true},
				"action":     {FieldName: "Action"},
			},
		},
	})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}

	if got, ok := upfOmittableAt(t, merged, "UpdateEventInput", "retryCount"); !ok || got != true {
		t.Errorf("retryCount omittable = %v (present %v), want true:\n%s", got, ok, merged)
	}
	models := unmarshalMap(t, merged)["models"].(map[string]any)
	if got := modelKeyAt(t, models, "UpdateEventInput", "retryCount", "fieldName"); got != "RetryCount" {
		t.Errorf("retryCount fieldName = %q, want %q — omittable must not displace the name", got, "RetryCount")
	}
	if got, ok := upfOmittableAt(t, merged, "UpdateEventInput", "action"); ok {
		t.Errorf("action omittable = %v, want no omittable key", got)
	}
}

// TestMerge_ModelFieldsSqlgenOmittableOverridesConsumer pins that sqlgen owns
// `omittable` on a field it marks: a consumer's `omittable: false` is
// overwritten in the merged config (the update translator reads the field
// through graphql.Omittable and would not compile otherwise), while the
// consumer's other per-field keys still win and its on-disk bytes are
// untouched.
func TestMerge_ModelFieldsSqlgenOmittableOverridesConsumer(t *testing.T) {
	consumer := []byte(`models:
  UpdateEventInput:
    fields:
      retryCount:
        omittable: false
        fieldName: Retries
`)
	original := string(consumer)
	merged, err := Merge(consumer, MergeInput{
		ModelFields: map[string]map[string]ModelField{
			"UpdateEventInput": {"retryCount": {FieldName: "RetryCount", Omittable: true}},
		},
	})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}

	if got, ok := upfOmittableAt(t, merged, "UpdateEventInput", "retryCount"); !ok || got != true {
		t.Errorf("merged omittable = %v (present %v), want sqlgen's true over the consumer's false:\n%s", got, ok, merged)
	}
	models := unmarshalMap(t, merged)["models"].(map[string]any)
	if got := modelKeyAt(t, models, "UpdateEventInput", "retryCount", "fieldName"); got != "Retries" {
		t.Errorf("consumer fieldName = %q, want it preserved as %q", got, "Retries")
	}
	if string(consumer) != original {
		t.Errorf("Merge modified the consumer bytes:\n%s", consumer)
	}
}
