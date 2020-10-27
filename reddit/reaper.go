package reddit

import (
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	// scheme is a map of TLS=[true|false] to the scheme for that setting.
	scheme = map[bool]string{
		true:  "https",
		false: "http",
	}
)

type reaperConfig struct {
	client     client
	parser     parser
	hostname   string
	reapSuffix string
	tls        bool
	rate       time.Duration
}

// reaper is a high level api for Reddit HTTP requests.
type reaper interface {
	// reap executes a GET request to Reddit and returns the elements from
	// the endpoint.
	reap(path string, values map[string]string) (Harvest, error)
	// sow executes a POST request to Reddit.
	sow(path string, values map[string]string) error
	// get_sow executes a POST request to Reddit
	// and returns the response, usually the posted item
	get_sow(path string, values map[string]string) (Submission, error)
}

type reaperImpl struct {
	cli        client
	parser     parser
	hostname   string
	reapSuffix string
	scheme     string
	rate       time.Duration
	last       time.Time
	mu         *sync.Mutex
}

func newReaper(c reaperConfig) reaper {
	return &reaperImpl{
		cli:        c.client,
		parser:     c.parser,
		hostname:   c.hostname,
		reapSuffix: c.reapSuffix,
		scheme:     scheme[c.tls],
		rate:       c.rate,
		mu:         &sync.Mutex{},
	}
}

func (r *reaperImpl) reap(path string, values map[string]string) (Harvest, error) {
	r.rateBlock()
	resp, err := r.cli.Do(
		&http.Request{
			Method: "GET",
			URL:    r.url(r.path(path, r.reapSuffix), values),
			Host:   r.hostname,
		},
	)
	if err != nil {
		return Harvest{}, err
	}

	comments, posts, messages, mores, err := r.parser.parse(resp)
	return Harvest{
		Comments: comments,
		Posts:    posts,
		Messages: messages,
		Mores:    mores,
	}, err
}

func (r *reaperImpl) sow(path string, values map[string]string) error {
	r.rateBlock()
	_, err := r.cli.Do(
		&http.Request{
			Method: "POST",
			Header: r.getHeaders(values),
			Host:   r.hostname,
			URL:    r.postURL(path),
			Body:   r.getBody(values),
		},
	)

	return err
}

func (r *reaperImpl) get_sow(path string, values map[string]string) (Submission, error) {
	r.rateBlock()
	values["api_type"] = "json"
	resp, err := r.cli.Do(
		&http.Request{
			Method: "POST",
			Header: r.getHeaders(values),
			Host:   r.hostname,
			URL:    r.postURL(path),
			Body:   r.getBody(values),
		},
	)

	if err != nil {
		return Submission{}, err
	}

	return r.parser.parse_submitted(resp)
}

func (r *reaperImpl) rateBlock() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if time.Since(r.last) < r.rate {
		<-time.After(r.last.Add(r.rate).Sub(time.Now()))
	}
	r.last = time.Now()
}

func (r *reaperImpl) url(path string, values map[string]string) *url.URL {
	return &url.URL{
		Scheme:   r.scheme,
		Host:     r.hostname,
		Path:     path,
		RawQuery: r.formatValues(values).Encode(),
	}
}

func (r *reaperImpl) postURL(path string) *url.URL {
	return &url.URL{
		Scheme: r.scheme,
		Host:   r.hostname,
		Path:   path,
	}
}

func (r *reaperImpl) path(p string, suff string) string {
	if strings.HasSuffix(p, suff) {
		return p
	}

	return p + suff
}

func (r *reaperImpl) formatValues(values map[string]string) url.Values {
	formattedValues := url.Values{}

	for key, value := range values {
		formattedValues[key] = []string{value}
	}

	return formattedValues
}

func (r *reaperImpl) getHeaders(values map[string]string) map[string][]string {
	headers := make(map[string][]string)
	b, _ := io.Copy(ioutil.Discard, strings.NewReader(r.formatValues(values).Encode()))

	headers["Content-Type"] = []string{"application/x-www-form-urlencoded"}
	headers["Content-Length"] = []string{strconv.Itoa(int(b))}

	return headers
}

func (r *reaperImpl) getBody(values map[string]string) io.ReadCloser {
	return ioutil.NopCloser(strings.NewReader(r.formatValues(values).Encode()))
}
