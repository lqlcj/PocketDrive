package files

import "testing"

func TestCleanPath(t *testing.T) {
	cases := map[string]string{
		"":                "",
		"/":               "",
		"a/b":             "a/b",
		"/a/b/":           "a/b",
		"../..":           "",
		"a/../../etc":     "etc",
		"..\\..\\windows": "windows",
		"a\\b":            "a/b",
		"./a":             "a",
	}
	for in, want := range cases {
		if got := CleanPath(in); got != want {
			t.Errorf("CleanPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidName(t *testing.T) {
	for _, bad := range []string{"", ".", "..", "a/b", `a\b`} {
		if err := validName(bad); err == nil {
			t.Errorf("validName(%q) = nil, want error", bad)
		}
	}
	if err := validName("正常文件.txt"); err != nil {
		t.Errorf("validName(normal) = %v", err)
	}
}
