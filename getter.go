package httpretry

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/cenkalti/backoff"
)

type Callback func(*http.Response, error)

type HttpGetter struct {
	Request        *http.Request
	Body           io.ReadCloser
	Attempts       int
	ContentLength  int64
	BytesRead      int64
	StatusCode     int
	Header         http.Header
	client         *http.Client
	b              *QuittableBackOff
	cb             Callback
	next           time.Duration
	expectedStatus int
}

func Getter(req *http.Request) *HttpGetter {
	return &HttpGetter{Request: req, expectedStatus: 200}
}

func (g *HttpGetter) Do() (int, http.Header) {
	if g.b == nil {
		g.SetBackOff(nil)
	}

	if g.client == nil {
		g.SetClient(nil)
	}

	if g.cb == nil {
		g.SetCallback(nil)
	}

	backoff.Retry(g.connect, g.b)
	return g.StatusCode, g.Header
}

func (g *HttpGetter) SetBackOff(b backoff.BackOff) {
	if b == nil {
		b = DefaultBackOff()
	}
	g.b = &QuittableBackOff{b: b}
}

func (g *HttpGetter) SetClient(c *http.Client) {
	if c == nil {
		g.client = http.DefaultClient
	} else {
		g.client = c
	}
}

func (g *HttpGetter) SetCallback(f Callback) {
	if f == nil {
		g.cb = cb
	} else {
		g.cb = f
	}
}

func (g *HttpGetter) Read(b []byte) (int, error) {
	if g.Body == nil {
		if err := g.connect(); err != nil {
			if g.next = g.b.NextBackOff(); g.next == backoff.Stop {
				return 0, err
			}

			time.Sleep(g.next)

			return 0, nil
		} else {
			g.b.Reset()
		}
	}

	read, err := g.Body.Read(b)
	g.BytesRead += int64(read)
	if err != nil {
		g.Close()

		if err != io.EOF {
			return read, nil
		}
	}

	return read, err
}

func (g *HttpGetter) Close() error {
	var err error
	if g.Body != nil {
		err = g.Body.Close()
		g.Body = nil
	}

	return err
}

func (g *HttpGetter) connect() error {
	if g.b.IsDone {
		return io.EOF
	}

	if g.BytesRead > 0 && g.ContentLength > 0 {
		g.Request.Header.Set(rangeHeader, fmt.Sprintf(rangeFormat, g.BytesRead, g.ContentLength-1))
	}

	res, err := g.client.Do(g.Request)
	g.Attempts += 1
	g.cb(res, err)
	if err != nil {
		return err
	}

	if res.StatusCode == 0 {
		return EmptyResponse
	}

	g.Body = res.Body

	if res.StatusCode == g.expectedStatus {
		if g.setResponse(res) {
			g.expectedStatus = 206
		}
	} else {
		if g.expectedStatus == 206 {
			g.Close()
		}

		if res.StatusCode < 500 || res.StatusCode > 599 {
			g.setResponse(res)
			g.b.Done()
		}

		return fmt.Errorf("Expected status code %d, got %d", g.expectedStatus, res.StatusCode)
	}

	return nil
}

func (g *HttpGetter) setResponse(res *http.Response) bool {
	if g.StatusCode > 0 {
		return false
	}

	g.StatusCode = res.StatusCode
	g.Header = res.Header
	if v := g.Header.Get(acceptHeader); v != acceptValue {
		g.b.Done()
	}

	i, _ := strconv.ParseInt(res.Header.Get(clenHeader), 10, 0)
	g.ContentLength = i
	return true
}

var (
	cb            = func(r *http.Response, e error) {}
	EmptyResponse = fmt.Errorf("Received response with status code 0")
)

const (
	acceptHeader = "Accept-Ranges"
	acceptValue  = "bytes"
	rangeHeader  = "Range"
	rangeFormat  = "bytes=%d-%d"
	clenHeader   = "Content-Length"
)
