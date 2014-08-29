package httpretry

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/cenkalti/backoff"
)

type HttpGetter struct {
	Client         *http.Client
	Request        *http.Request
	Body           io.ReadCloser
	Attempts       int
	ContentLength  int64
	BytesRead      int64
	StatusCode     int
	Header         http.Header
	b              *QuittableBackOff
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

	if g.Client == nil {
		g.Client = http.DefaultClient
	}

	backoff.Retry(g.do, g.b)
	return g.StatusCode, g.Header
}

func (g *HttpGetter) SetBackOff(b backoff.BackOff) {
	if b == nil {
		b = DefaultBackOff()
	}
	g.b = &QuittableBackOff{b: b}
}

func (g *HttpGetter) Read(b []byte) (int, error) {
	if g.Body == nil {
		if err := g.do(); err != nil {
			if g.next = g.b.NextBackOff(); g.next == backoff.Stop {
				return 0, err
			}

			time.Sleep(g.next)

			return 0, nil
		}
	}

	read, err := g.Body.Read(b)
	g.BytesRead += int64(read)
	if err != nil && err != io.EOF {
		g.Close()
		return read, nil
	}

	if err == io.EOF {
		g.Close()
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

func (g *HttpGetter) do() error {
	if g.b.IsDone {
		return io.EOF
	}

	if g.BytesRead > 0 {
		g.Request.Header.Set(rangeHeader, fmt.Sprintf(rangeFormat, g.BytesRead, g.ContentLength-1))
	}

	res, err := g.Client.Do(g.Request)
	g.Attempts += 1
	if err != nil {
		return err
	}

	g.Body = res.Body

	if res.StatusCode == g.expectedStatus {
		if g.StatusCode < 1 {
			g.setResponse(res)
			g.expectedStatus = 206
		}
	} else {
		if g.expectedStatus == 206 {
			g.Close()
		}

		if res.StatusCode > 399 && res.StatusCode < 500 {
			g.setResponse(res)
			g.b.Done()
		}

		return fmt.Errorf("Expected status code %d, got %d", g.expectedStatus, res.StatusCode)
	}

	return nil
}

func (g *HttpGetter) setResponse(res *http.Response) {
	g.StatusCode = res.StatusCode
	g.Header = res.Header
	if v := g.Header.Get(acceptHeader); v != acceptValue {
		g.b.Done()
	}

	i, _ := strconv.ParseInt(res.Header.Get(clenHeader), 10, 0)
	g.ContentLength = i
}

const (
	acceptHeader = "Accept-Ranges"
	acceptValue  = "bytes"
	rangeHeader  = "Range"
	rangeFormat  = "bytes=%d-%d"
	clenHeader   = "Content-Length"
)
