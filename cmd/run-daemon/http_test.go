package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsLoopback(t *testing.T) {
	for host, want := range map[string]bool{
		"localhost:19230": true,
		"127.0.0.1:19230": true,
		"[::1]:19230":     true,
		"localhost":       true,
		"127.0.0.1":       true,
		"evil.com:19230":  false,
		"example.com":     false,
		"127.0.0.2:19230": false,
	} {
		if got := isLoopback(host); got != want {
			t.Errorf("isLoopback(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestLocalOnly(t *testing.T) {
	var called bool
	h := localOnly(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	cases := []struct {
		name       string
		host       string
		secFetch   string
		wantStatus int
	}{
		{"loopback host", "localhost:19230", "", http.StatusOK},
		{"ip host", "127.0.0.1:19230", "", http.StatusOK},
		{"same-origin browser", "localhost:19230", "same-origin", http.StatusOK},
		{"no sec-fetch (curl)", "localhost:19230", "", http.StatusOK},
		{"rebind host", "evil.com:19230", "", http.StatusForbidden},
		{"cross-site browser", "localhost:19230", "cross-site", http.StatusForbidden},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			called = false
			r := httptest.NewRequest("GET", "/list", nil)
			r.Host = tc.host
			if tc.secFetch != "" {
				r.Header.Set("Sec-Fetch-Site", tc.secFetch)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.wantStatus {
				t.Errorf("status %d, want %d", w.Code, tc.wantStatus)
			}
			if tc.wantStatus == http.StatusOK && !called {
				t.Error("handler wasn't called")
			}
		})
	}
}
