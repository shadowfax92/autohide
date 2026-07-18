package daemon

import "testing"

func TestNormalizeLegacyAppName(t *testing.T) {
	cases := map[string]string{
		"":                "",
		"   ":             "",
		"missing value":   "",
		"MISSING VALUE":   "",
		" missing value ": "",
		" Slack ":         "Slack",
	}
	for input, want := range cases {
		if got := normalizeLegacyAppName(input); got != want {
			t.Errorf("normalizeLegacyAppName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestHideAppIgnoresMissingValue(t *testing.T) {
	for _, name := range []string{"", "missing value", "MISSING VALUE"} {
		if err := HideApp(name); err != nil {
			t.Errorf("HideApp(%q) = %v", name, err)
		}
	}
}
