package httpretry

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientWithTimeout(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "5")
		w.WriteHeader(200)
		w.Write([]byte("a"))
		time.Sleep(time.Duration(100 * time.Millisecond))
		w.Write([]byte("b"))
		time.Sleep(time.Duration(100 * time.Millisecond))
		w.Write([]byte("c"))
		time.Sleep(time.Duration(200 * time.Millisecond))
		w.Write([]byte("d"))
		time.Sleep(time.Duration(100 * time.Millisecond))
		w.Write([]byte("e"))
	}))
	defer ts.Close()

	req, err := http.NewRequest("GET", ts.URL, nil)
	if err != nil {
		t.Fatal(err)
	}

	client := ClientWithTimeout(time.Duration(150 * time.Millisecond))
	res, err := client.Do(req)
	if err == nil {
		by, err := ioutil.ReadAll(res.Body)
		res.Body.Close()
		if err != nil {
			by = []byte(err.Error())
		}

		t.Fatalf("Expected error, got: %d // %s", res.StatusCode, string(by))
	}

	if e := err.Error(); !strings.Contains(e, "timeout") {
		t.Fatalf("Unexpected error: %s", e)
	}
}
