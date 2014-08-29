/*
Package httpretry implements a helper for retrying failed HTTP requests with
Range headers to emit a clean stream of data.  Requests are retried after errors
with exponential backoff.

    req, _ := http.NewRequest("GET", "some/uri", nil)
    getter := httpretry.Getter(req)
    defer getter.Close()

    // start the request
    status, head := getter.Do()

    // read the data
    io.Copy(someWriter, getter)

Before the getter starts the request with Do(), you can call the Set* functions
to configure how the Getter works.

You can configure the backoff settings with the backoff package:

    // import "github.com/cenkalti/backoff"

    b := backoff.NewExponentialBackOff()
    b.InitialInterval = time.Duration(100 * time.Millisecond)
    b.MaxInterval = time.Second
    b.MaxElapsedTime = time.Duration(5 * time.Second)

    req, _ := http.NewRequest("GET", "some/uri", nil)
    getter := httpretry.Getter(req)

    getter.SetBackOff(b)

You can pass in an *http.Client if you don't want to use http.DefaultClient.

    req, _ := http.NewRequest("GET", "some/uri", nil)
    getter := httpretry.Getter(req)
    getter.SetClient(&http.Client{})

You can set a callback to see every response, for logging purposes.

    // import "github.com/peterbourgon/g2s"

    req, _ := http.NewRequest("GET", "some/uri", nil)
    getter := httpretry.Getter(req)
    g.SetCallback(func(res *http.Response, err error) {
      key := "prefix"
      if err == nil {
        key += fmt.Sprintf(".code.%d", res.StatusCode)
      } else {
        key += ".error"
      }
      statter.Counter(1.0, key, 1)
    })
*/
package httpretry
