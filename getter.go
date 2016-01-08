package httpretry

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"io/ioutil"
	"net/http"
	"strconv"
	"time"

	"github.com/cenkalti/backoff"
)

type ResponseCallback func(*http.Response, error)
type CloseCallback func(*HttpGetter)

// An HttpGetter is a wrapper around an HTTP Client that handles retries for
// certain types of errors.  It implements the io.ReadCloser interface, and
// must be closed to clean up any lingering connections.  However, Do() must
// be called before the first Read() is attempted.
//
// 4xx responses are considered errors due to a bad request by the client, and
// will not be restarted.
//
// Go errors and 5xx responses will be retried, even if the connection times
// out, or drops before the entire response has been received.  Retries are
// based on the Range header.  So, servers must advertise their capability to
// fetch partial with the Accept-Ranges.
//
// A successful response should have a status of 200 if no Range header was
// sent, or 206.
type HttpGetter struct {
	Request       *http.Request
	Body          io.ReadCloser
	Attempts      int
	ContentLength int64
	BytesRead     int64
	StatusCode    int
	Header        http.Header
	client        *http.Client
	hasher        hash.Hash
	b             *QuittableBackOff
	rcb           ResponseCallback
	ccb           CloseCallback
	next          time.Duration
	closed        bool
}

// Getter initializes the *HttpGetter.
func Getter(req *http.Request) *HttpGetter {
	return &HttpGetter{Request: req}
}

// Do returns the status code and response header for the first successful
// response.  Any Go errors or 5xx status codes will trigger retries.
func (g *HttpGetter) Do() (int, http.Header) {
	if g.b == nil {
		g.SetBackOff(nil)
	}

	if g.client == nil {
		g.SetClient(nil)
	}

	if g.hasher == nil {
		g.SetHash(nil)
	}

	if g.rcb == nil {
		g.OnResponse(nil)
	}

	if g.ccb == nil {
		g.OnClose(nil)
	}

	backoff.Retry(g.connect, g.b)
	return g.StatusCode, g.Header
}

// SetBackOff sets the backoff configuration for this *HttpGetter.  If nil,
// DefaultBackoff() is called instead.
func (g *HttpGetter) SetBackOff(b backoff.BackOff) {
	if b == nil {
		b = DefaultBackOff()
	}
	g.b = &QuittableBackOff{b: b}
}

// SetClient sets the HTTP Client for this *HttpGetter.  If nil,
// http.DefaultClient is used.
func (g *HttpGetter) SetClient(c *http.Client) {
	if c == nil {
		g.client = http.DefaultClient
	} else {
		g.client = c
	}
}

// SetHash sets the Hash used to calculate a signature of the content read from
// this *HttpGetter.  If nil, a new sha256 hash is created.
func (g *HttpGetter) SetHash(h hash.Hash) {
	if h == nil {
		g.hasher = sha256.New()
	} else {
		g.hasher = h
	}
}

// OnResponse sets a function to be called after every attempted HTTP response.
func (g *HttpGetter) OnResponse(f ResponseCallback) {
	if f == nil {
		g.rcb = rcb
	} else {
		g.rcb = f
	}
}

// OnClose sets a function to be called after the getter has closed.
func (g *HttpGetter) OnClose(f CloseCallback) {
	if f == nil {
		g.ccb = ccb
	} else {
		g.ccb = f
	}
}

// Read implements the io.Reader interface.  If a non EOF error is returned,
// the HTTP body is closed, and no Go error is returned so that Read() can
// get called again.  The backoff retry logic is used to re-establish HTTP
// connections.  Once the number of retries has been exhausted, the Go error
// is finally returned.
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
	if read > 0 {
		g.BytesRead += int64(read)
		g.hasher.Write(b[:read])
	}
	if err != nil {
		g.reset()

		// return nil so that Read() is called again.
		if err != io.EOF {
			return read, nil
		}
	}

	return read, err
}

// Sha256 gets the hex encoded SHA 256 signature of the content that's been read
// so far.
func (g *HttpGetter) Sha256() string {
	return hex.EncodeToString(g.hasher.Sum(nil))
}

// Close cleans up any lingering HTTP connections.  CLosing the getter prevents
// further HTTP requests from being attempted.
func (g *HttpGetter) Close() error {
	g.b.Done()
	if !g.closed {
		g.ccb(g)
		g.closed = true
	}

	return g.reset()
}

// reset resets the Body io.ReadCloser
func (g *HttpGetter) reset() error {
	var err error
	if g.Body != nil {
		err = g.Body.Close()
		g.Body = nil
	}
	return err
}

// connect attempts to make the http response.  If any Go error is returned, or
// a status other than 200 or 206 is encountered, an error is returned to signal
// to the *HttpGetter to retry later.
func (g *HttpGetter) connect() error {
	// Non 5xx statuses or the lack of an Accept-Ranges response header will
	// prevent future retries.
	if g.b.IsDone {
		return io.EOF
	}

	expectedStatus := 200

	if g.BytesRead > 0 && g.ContentLength > 0 {
		expectedStatus = 206
		g.Request.Header.Set(rangeHeader, fmt.Sprintf(rangeFormat, g.BytesRead, g.ContentLength-1))
	}

	res, err := g.client.Do(g.Request)
	g.Attempts += 1
	g.rcb(res, err)
	if err != nil {
		return err
	}

	if res.StatusCode == 0 {
		return EmptyResponse
	}

	g.Body = res.Body

	// successful response
	if res.StatusCode == expectedStatus {
		g.setResponse(res)
	} else {
		// if we're looking for a partial response, just close and retry later.
		if expectedStatus == 206 {
			g.reset()
		}

		// if it's not a 5xx, stop retries.
		if res.StatusCode > 0 && (res.StatusCode < 500 || res.StatusCode > 599) {
			g.setResponse(res)
			g.b.Done()
		} else {
			// Drain the body, necessary for go <= 1.3
			io.Copy(ioutil.Discard, res.Body)
			res.Body.Close()
		}

		return fmt.Errorf("Expected status code %d, got %d", expectedStatus, res.StatusCode)
	}

	return nil
}

// setResponse sets the response status, header, and content length from the
// first successful response.
func (g *HttpGetter) setResponse(res *http.Response) {
	if g.StatusCode > 0 {
		return
	}

	g.StatusCode = res.StatusCode
	g.Header = res.Header
	if v := g.Header.Get(acceptHeader); v != acceptValue {
		g.b.Done()
	}

	i, _ := strconv.ParseInt(res.Header.Get(clenHeader), 10, 0)
	g.ContentLength = i
}

var (
	rcb           = func(r *http.Response, e error) {}
	ccb           = func(g *HttpGetter) {}
	EmptyResponse = fmt.Errorf("Received response with status code 0")
)

const (
	acceptHeader = "Accept-Ranges"
	acceptValue  = "bytes"
	rangeHeader  = "Range"
	rangeFormat  = "bytes=%d-%d"
	clenHeader   = "Content-Length"
)
