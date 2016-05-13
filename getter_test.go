package httpretry

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cenkalti/backoff"
)

func TestRetry(t *testing.T) {
	t.Parallel()

	numRequests := 0
	mutex := &sync.Mutex{}
	content := []byte("0123456789")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mutex.Lock()
		numRequests += 1
		mutex.Unlock()

		status := 200

		i := 0
		v := r.Header.Get("Range")
		if strings.HasPrefix(v, "bytes=") {
			i, _ = strconv.Atoi(strings.SplitN(v, "-", 2)[0][6:])
		}

		head := w.Header()
		head.Set("Accept-Ranges", "bytes")
		head.Set("Content-Type", "text/plain")

		if i > 0 {
			head.Set("Content-Range", fmt.Sprintf("bytes %d-4/4", i))
			status = 206
		}

		if numRequests%2 == 0 {
			head.Set("Content-Length", "4")
			w.WriteHeader(500)
			w.Write([]byte("BOOM"))
			return
		}

		head.Set("Content-Length", fmt.Sprintf("%d", len(content)-i))
		w.WriteHeader(status)
		w.Write(content[i : i+1])
	}))
	defer ts.Close()

	req, err := http.NewRequest("GET", ts.URL, nil)
	if err != nil {
		t.Fatal(err)
	}

	reader := testGetter(t, req)
	seenCodes := trackSeenCodes(reader)
	code, head := reader.Do()

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

	if written != 10 {
		t.Errorf("Wrote %d", written)
	}

	if b := buf.String(); b != "0123456789" {
		t.Errorf("Got %s", b)
	}

	if s := reader.Sha256(); s != "84d89877f0d4041efb6bf91a16f0248f2fd573e6af05c19f96bedb9f882f7882" {
		t.Errorf("Bad SHA256: %s", s)
	}

	if numRequests < 2 {
		t.Errorf("Only made %d request(s)?", numRequests)
	}

	assertSeenCodes(t, seenCodes, 200, 206, 500)

	t.Logf("requests made: %d", numRequests)

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

	reader := testGetter(t, req)
	code, head := reader.Do()

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

	if s := reader.Sha256(); s != "2689367b205c16ce32ed4200942b8b8b1e262dfc70d9bc9fbc77c49699a4f1df" {
		t.Errorf("Bad SHA256: %s", s)
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

	reader := testGetter(t, req)
	code, head := reader.Do()

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

	if s := reader.Sha256(); s != "65c74c15a686187bb6bbf9958f494fc6b80068034a659a9ad44991b08c58f2d2" {
		t.Errorf("Bad SHA256: %s", s)
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
		reader := testGetter(t, req)
		code, head := reader.Do()

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

func trackSeenCodes(g *HttpGetter) map[int]bool {
	seenCodes := make(map[int]bool)
	mu := &sync.Mutex{}
	g.OnResponse(func(res *http.Response, err error) {
		mu.Lock()
		if res == nil {
			seenCodes[0] = true
		} else {
			seenCodes[res.StatusCode] = true
		}
		mu.Unlock()
	})
	return seenCodes
}

func assertSeenCodes(t *testing.T, seenCodes map[int]bool, expectedCodes ...int) {
	for _, code := range expectedCodes {
		if !seenCodes[code] {
			t.Errorf("Expected to see response %d", code)
		}
	}
}

func testGetter(t *testing.T, req *http.Request, expectedCodes ...int) *HttpGetter {
	g := Getter(req)
	g.SetBackOff(zeroBackOff)
	return g
}
