package tracecommon

import (
	"net/http/httptest"
	"testing"
)

func TestGetProfileMode(t *testing.T) {
	f := func(query, modeExpected string, errExpected bool) {
		t.Helper()

		r := httptest.NewRequest("GET", "/select/tempo/api/profile?"+query, nil)
		mode, err := GetProfileMode(r)
		if errExpected {
			if err == nil {
				t.Fatalf("expecting non-nil error for %q", query)
			}
			return
		}
		if err != nil {
			t.Fatalf("unexpected error for %q: %s", query, err)
		}
		if mode != modeExpected {
			t.Fatalf("unexpected mode for %q; got %q; want %q", query, mode, modeExpected)
		}
	}

	f("", "explain", false)
	f("mode=explain", "explain", false)
	f("mode=ANALYZE", "analyze", false)
	f("analyze=true", "analyze", false)
	f("analyze=1", "analyze", false)
	f("analyze=false", "explain", false)
	f("mode=explain&analyze=true", "explain", false)

	f("mode=foo", "", true)
	f("analyze=foo", "", true)
}
