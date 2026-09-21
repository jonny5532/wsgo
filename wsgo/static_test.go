package wsgo

import "testing"

func TestPathHasPrefix(t *testing.T) {
	cases := []struct {
		path   string
		prefix string
		want   bool
	}{
		{"/media", "/media", true},
		{"/media/x", "/media", true},
		{"/media/", "/media", true},
		{"/mediaevil/x", "/media", false},
		{"/mediaevil", "/media", false},
		{"/x", "/", true},
		{"/", "/", true},
		{"/x", "", true},
		{"/mediax/media", "/media", false},
	}

	for _, c := range cases {
		if got := pathHasPrefix(c.path, c.prefix); got != c.want {
			t.Errorf("pathHasPrefix(%q, %q) = %v, want %v", c.path, c.prefix, got, c.want)
		}
	}
}

func TestIsPathWithin(t *testing.T) {
	cases := []struct {
		path string
		root string
		want bool
	}{
		{"/srv/web", "/srv/web", true},
		{"/srv/web/x", "/srv/web", true},
		{"/srv/web-secret/x", "/srv/web", false},
		{"/srv/websecret", "/srv/web", false},
		{"/etc/passwd", "/srv/web", false},
		{"/etc/passwd", "/", true},
	}

	for _, c := range cases {
		if got := isPathWithin(c.path, c.root); got != c.want {
			t.Errorf("isPathWithin(%q, %q) = %v, want %v", c.path, c.root, got, c.want)
		}
	}
}

func TestAcceptsGzip(t *testing.T) {
	cases := []struct {
		header string
		want   bool
	}{
		{"", false},
		{"gzip", true},
		{"gzip, deflate", true},
		{"deflate, gzip;q=0.5", true},
		{"gzip;q=0", false},
		{"gzip; q=0", false},
		{"gzip;q=0.0", false},
		{"deflate", false},
		{"identity", false},
		{"*", true},
		{"*, gzip;q=0", false},
		{"br, *;q=0", false},
	}

	for _, c := range cases {
		if got := acceptsGzip(c.header); got != c.want {
			t.Errorf("acceptsGzip(%q) = %v, want %v", c.header, got, c.want)
		}
	}
}
