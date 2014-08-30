package httpretry

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/cenkalti/backoff"
)

func TestRetry(t *testing.T) {
	t.Parallel()

	i := 0
	requests := []func(w http.ResponseWriter, r *http.Request){
		func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(time.Second)
			writeTestData(w, 404, "never reached")
		},
		func(w http.ResponseWriter, r *http.Request) {
			if v := r.Header.Get("Range"); v != "" {
				t.Errorf("Unexpected Range header on request %d: %s", v, i)
			}

			head := w.Header()
			head.Set("Test-Request", strconv.Itoa(i))
			head.Set("Content-Type", "text/plain")
			head.Set("Content-Length", "4")
			w.WriteHeader(500)
			w.Write([]byte("boom"))
		},
		func(w http.ResponseWriter, r *http.Request) {
			if v := r.Header.Get("Range"); v != "" {
				t.Errorf("Unexpected Range header on request %d: %s", v, i)
			}

			head := w.Header()
			head.Set("Test-Request", strconv.Itoa(i))
			head.Set("Accept-Ranges", "bytes")
			head.Set("Content-Type", "text/plain")
			head.Set("Content-Length", "5")
			w.WriteHeader(200)
			w.Write([]byte("ab"))
		},
		func(w http.ResponseWriter, r *http.Request) {
			if v := r.Header.Get("Range"); v != "bytes=2-4" {
				t.Errorf("Unexpected Range header on request %d: %s", v, i)
			}

			head := w.Header()
			head.Set("Test-Request", strconv.Itoa(i))
			head.Set("Content-Range", "bytes 2-4/4")
			head.Set("Accept-Ranges", "bytes")
			head.Set("Content-Type", "text/plain")
			head.Set("Content-Length", "3")
			w.WriteHeader(206)
			w.Write([]byte("cd"))
		},
		func(w http.ResponseWriter, r *http.Request) {
			if v := r.Header.Get("Range"); v != "bytes=4-4" {
				t.Errorf("Unexpected Range header on request %d: %s", v, i)
			}

			time.Sleep(time.Second)
			writeTestData(w, 404, "never reached")
		},
		func(w http.ResponseWriter, r *http.Request) {
			if v := r.Header.Get("Range"); v != "bytes=4-4" {
				t.Errorf("Unexpected Range header on request %d: %s", v, i)
			}

			head := w.Header()
			head.Set("Test-Request", strconv.Itoa(i))
			head.Set("Content-Type", "text/plain")
			head.Set("Content-Length", "4")
			w.WriteHeader(500)
			w.Write([]byte("boom"))
		},
		func(w http.ResponseWriter, r *http.Request) {
			if v := r.Header.Get("Range"); v != "bytes=4-4" {
				t.Errorf("Unexpected Range header on request %d: %s", v, i)
			}

			head := w.Header()
			head.Set("Test-Request", strconv.Itoa(i))
			head.Set("Content-Range", "bytes 4-4/4")
			head.Set("Accept-Ranges", "bytes")
			head.Set("Content-Type", "text/plain")
			head.Set("Content-Length", "1")
			w.WriteHeader(206)
			w.Write([]byte("e"))
		},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if i < len(requests) {
			requests[i](w, r)
			i += 1
		} else {
			head := w.Header()
			head.Set("Content-Type", "text/plain")
			head.Set("Content-Length", "7")
			w.WriteHeader(404)
			w.Write([]byte("missing"))
		}
	}))
	defer ts.Close()

	req, err := http.NewRequest("GET", ts.URL, nil)
	if err != nil {
		t.Fatal(err)
	}

	code, head, reader := testGetter(t, req, 0, 0, 500, 200, 206, 0, 500, 206)

	if code != 200 {
		t.Errorf("Unexpected status %d", code)
	}

	if ctype := head.Get("Content-Type"); ctype != "text/plain" {
		t.Errorf("Unexpected Content Type: %s", ctype)
	}

	buf := &bytes.Buffer{}
	written, err := io.Copy(buf, reader)
	if err != nil {
		t.Errorf("Copy error: %s", err)
	}

	if written != 5 {
		t.Errorf("Wrote %d", written)
	}

	if b := buf.String(); b != "abcde" {
		t.Errorf("Got %s", b)
	}

	reader.Close()
}

func TestSingleSuccess(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTestData(w, 200, "ok")
	}))
	defer ts.Close()

	req, err := http.NewRequest("GET", ts.URL, nil)
	if err != nil {
		t.Fatal(err)
	}

	code, head, reader := testGetter(t, req)

	if code != 200 {
		t.Errorf("Unexpected status %d", code)
	}

	if ctype := head.Get("Content-Type"); ctype != "text/plain" {
		t.Errorf("Unexpected Content Type: %s", ctype)
	}

	buf := &bytes.Buffer{}
	written, err := io.Copy(buf, reader)
	if err != nil {
		t.Errorf("Copy error: %s", err)
	}

	if written != 2 {
		t.Errorf("Wrote %d", written)
	}

	if b := buf.String(); b != "ok" {
		t.Errorf("Got %s", b)
	}

	reader.Close()
}

func TestSkipRetryWithoutAcceptRange(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		head := w.Header()
		head.Set("Content-Type", "text/plain")
		head.Set("Content-Length", "2")
		w.WriteHeader(200)
		w.Write([]byte("o"))
	}))
	defer ts.Close()

	req, err := http.NewRequest("GET", ts.URL, nil)
	if err != nil {
		t.Fatal(err)
	}

	code, head, reader := testGetter(t, req)

	if code != 200 {
		t.Errorf("Unexpected status %d", code)
	}

	if ctype := head.Get("Content-Type"); ctype != "text/plain" {
		t.Errorf("Unexpected Content Type: %s", ctype)
	}

	buf := &bytes.Buffer{}
	written, err := io.Copy(buf, reader)
	if err != nil {
		t.Errorf("Copy error: %s", err)
	}

	if written != 1 {
		t.Errorf("Wrote %d", written)
	}

	if b := buf.String(); b != "o" {
		t.Errorf("Got %s", b)
	}

	reader.Close()
}

func TestSkipRetryWith400(t *testing.T) {
	t.Parallel()
	status := 200
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTestData(w, status, "client error")
	}))
	defer ts.Close()

	req, err := http.NewRequest("GET", ts.URL, nil)
	if err != nil {
		t.Fatal(err)
	}

	for status = 400; status < 500; status++ {
		code, head, reader := testGetter(t, req)

		if code != status {
			t.Errorf("Expected status %d, got %d", status, code)
		}

		if ctype := head.Get("Content-Type"); ctype != "text/plain" {
			t.Fatalf("Unexpected Content Type: %s", ctype)
		}

		buf := &bytes.Buffer{}
		written, err := io.Copy(buf, reader)
		if err != nil {
			t.Errorf("Copy error: %s", err)
		}

		if written != 12 {
			t.Errorf("Wrote %d", written)
		}

		if b := buf.String(); b != "client error" {
			t.Errorf("Got %s", b)
		}

		reader.Close()
	}
}

func writeTestData(w http.ResponseWriter, status int, body string) {
	by := []byte(body)
	head := w.Header()
	head.Set("Accept-Ranges", "bytes")
	head.Set("Content-Type", "text/plain")
	head.Set("Content-Length", strconv.Itoa(len(by)))
	w.WriteHeader(status)
	w.Write(by)
}

var zeroBackOff = &backoff.ZeroBackOff{}

func init() {
	tport := http.DefaultTransport.(*http.Transport)
	tport.ResponseHeaderTimeout = 500 * time.Millisecond
}

func testGetter(t *testing.T, req *http.Request, expectedCodes ...int) (int, http.Header, *HttpGetter) {
	g := Getter(req)
	expectedAttempts := len(expectedCodes)
	if expectedAttempts > 0 {
		i := 0
		g.OnResponse(func(res *http.Response, err error) {
			if i < len(expectedCodes) {
				exp := expectedCodes[i]
				if exp == 0 {
					if err == nil {
						t.Errorf("Expected error for request %d", i)
					}
				} else {
					if res != nil && exp != res.StatusCode {
						t.Errorf("Request %d expected code %d, got %d", i, exp, res.StatusCode)
					}
				}
			} else {
				t.Errorf("Request %d was unexpected", i)
			}
			i += 1
		})

		g.OnClose(func(g *HttpGetter) {
			if g.Attempts != expectedAttempts {
				t.Errorf("Expected %d attempts, got %d", expectedAttempts, g.Attempts)
			}
		})
	}

	g.SetBackOff(zeroBackOff)
	s, h := g.Do()
	return s, h, g
}
