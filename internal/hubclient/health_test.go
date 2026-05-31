package hubclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCheck_Healthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	status, err := Check(context.Background(), srv.URL+"/healthz")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !status.Reachable {
		t.Fatal("expected Reachable=true")
	}
	if status.Status != "ok" {
		t.Fatalf("Status = %q, want %q", status.Status, "ok")
	}
}

func TestCheck_Healthy_NonJSONBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("OK"))
	}))
	defer srv.Close()

	status, err := Check(context.Background(), srv.URL+"/healthz")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !status.Reachable {
		t.Fatal("a 2xx with a non-JSON body should still be Reachable")
	}
	if status.Status != "" {
		t.Fatalf("Status = %q, want empty", status.Status)
	}
}

func TestCheck_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	status, err := Check(context.Background(), srv.URL+"/healthz")
	if err == nil {
		t.Fatal("expected an error for HTTP 503")
	}
	if status.Reachable {
		t.Fatal("a non-2xx response must not be Reachable")
	}
}

func TestCheck_Unreachable(t *testing.T) {
	// Stand a server up then tear it down to get a URL that refuses connections.
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	status, err := Check(ctx, url+"/healthz")
	if err == nil {
		t.Fatal("expected an error contacting a closed server")
	}
	if status.Reachable {
		t.Fatal("a refused connection must not be Reachable")
	}
}

func TestCheck_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	status, err := Check(ctx, srv.URL+"/healthz")
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if status.Reachable {
		t.Fatal("a timed-out probe must not be Reachable")
	}
}

func TestCheck_RefusesRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://example.com/elsewhere", http.StatusFound)
	}))
	defer srv.Close()

	status, err := Check(context.Background(), srv.URL+"/healthz")
	if err == nil {
		t.Fatal("expected an error when the health endpoint redirects")
	}
	if status.Reachable {
		t.Fatal("a redirecting endpoint must not be Reachable")
	}
}
