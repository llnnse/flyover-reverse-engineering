package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStatusClassification(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			w.Write([]byte("data"))
		case "/missing":
			w.WriteHeader(http.StatusNotFound)
		case "/forbidden":
			w.WriteHeader(http.StatusForbidden)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	if data, err := Get(srv.URL + "/ok"); err != nil || string(data) != "data" {
		t.Fatalf("ok: %q %v", data, err)
	}
	_, err := Get(srv.URL + "/missing")
	if !IsNotFound(err) || IsAuthError(err) {
		t.Errorf("404 misclassified: %v", err)
	}
	_, err = Get(srv.URL + "/forbidden")
	if !IsAuthError(err) || IsNotFound(err) {
		t.Errorf("403 misclassified: %v", err)
	}
	_, err = Get(srv.URL + "/broken")
	if IsAuthError(err) || IsNotFound(err) || err == nil {
		t.Errorf("500 misclassified: %v", err)
	}
}
