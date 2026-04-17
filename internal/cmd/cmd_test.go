package cmd

import "testing"

func TestFirstLine(t *testing.T) {
	cases := map[string]string{
		"":              "",
		"single":        "single",
		"first\nsecond": "first",
		"\nempty-first": "",
		"trailing-nl\n": "trailing-nl",
	}
	for in, want := range cases {
		if got := firstLine(in); got != want {
			t.Errorf("firstLine(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBytesTrim(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"hello", "hello"},
		{"hello\n", "hello"},
		{"hello\r\n", "hello"},
		{"hello  ", "hello"},
		{"", ""},
		{"\n\n", ""},
	}
	for _, c := range cases {
		if got := string(bytesTrim([]byte(c.in))); got != c.want {
			t.Errorf("bytesTrim(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPickProvider(t *testing.T) {
	ok := []string{"azure", "az"}
	for _, n := range ok {
		if _, err := pickProvider(n); err != nil {
			t.Errorf("pickProvider(%q) unexpected err: %v", n, err)
		}
	}

	notYet := []string{"gcp", "aws"}
	for _, n := range notYet {
		if _, err := pickProvider(n); err == nil {
			t.Errorf("pickProvider(%q) expected coming-soon error", n)
		}
	}

	if _, err := pickProvider("digitalocean"); err == nil {
		t.Error("pickProvider(unknown) expected error")
	}
}
