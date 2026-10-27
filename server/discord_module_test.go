package server

import (
	"strings"
	"testing"
)

func TestEscapeMentions(t *testing.T) {
	for _, in := range []string{"@everyone hi", "@here", "<@123456>", "<@&987>", "<@!42>"} {
		out := escapeMentions(in)
		// Discord only parses a mention with nothing between '@' and the rest
		if strings.Count(out, "@") != strings.Count(out, "@\u200b") {
			t.Errorf("escapeMentions(%q) = %q still mentions", in, out)
		}
		if strings.ReplaceAll(out, "​", "") != in {
			t.Errorf("escapeMentions(%q) = %q changed the text", in, out)
		}
	}
}
