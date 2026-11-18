package event_test

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/event"
)

// TestConfigOnErrorDefault pins the event.Config default for OnError: with
// OnError unset, the resolved handler logs through the standard log package,
// whose destination is stderr unless the program redirects it.
func TestConfigOnErrorDefault(t *testing.T) {
	t.Run("unset OnError logs the failure through the standard logger", func(t *testing.T) {
		var buf bytes.Buffer
		prevOut, prevFlags := log.Writer(), log.Flags()
		log.SetOutput(&buf)
		log.SetFlags(0)
		t.Cleanup(func() {
			log.SetOutput(prevOut)
			log.SetFlags(prevFlags)
		})

		cfg := event.Config{}
		onError := cfg.ResolveOnError()
		onError(context.Background(), []event.Event{{ID: "evt-1"}, {ID: "evt-2"}}, errors.New("broker unreachable"))

		got := buf.String()
		if !strings.Contains(got, "broker unreachable") {
			t.Errorf("default OnError logged %q, want a log line carrying the publish error %q", got, "broker unreachable")
		}
	})
}
