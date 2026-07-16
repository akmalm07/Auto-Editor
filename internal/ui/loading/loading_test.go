package loading

import (
	"bytes"
	"testing"
)

func TestSessionFallbackPrintsPlainMessagesWithoutTTY(t *testing.T) {
	var out bytes.Buffer
	session := New(&out, Settings{Enabled: true, Style: "ascii"})

	session.Start("Loading config")
	session.Update("Loading manifest")
	session.Stop("done")

	got := out.String()
	want := "Loading config\nLoading manifest\ndone\n"
	if got != want {
		t.Fatalf("unexpected output:\nwant:\n%q\ngot:\n%q", want, got)
	}
}
