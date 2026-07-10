package main

import "testing"

// TestServerURL covers turning a net/http listen address into a browsable URL:
// an empty or wildcard host must become localhost, a concrete host is kept.
func TestServerURL(t *testing.T) {
	cases := []struct {
		addr string
		want string
	}{
		{":8080", "http://localhost:8080"},
		{"0.0.0.0:8080", "http://localhost:8080"},
		{"[::]:8080", "http://localhost:8080"},
		{"127.0.0.1:9000", "http://127.0.0.1:9000"},
		{"localhost:8080", "http://localhost:8080"},
		{"192.168.1.5:3000", "http://192.168.1.5:3000"},
	}
	for _, c := range cases {
		if got := serverURL(c.addr); got != c.want {
			t.Errorf("serverURL(%q) = %q, want %q", c.addr, got, c.want)
		}
	}
}
