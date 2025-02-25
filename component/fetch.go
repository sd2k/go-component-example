package main

import (
	"fmt"
	"io"
	"net/http"

	"github.com/sd2k/go-component-example/internal/sd2k/go-component-example/fetcher"
	"go.bytecodealliance.org/cm"
)

type FetchResult = cm.Result[string, string, string]

func fetch(url string) (string, error) {
	r, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("error making request %v", err)
	}
	defer r.Body.Close()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return "", fmt.Errorf("error reading body %v", err)
	}
	return string(body), nil
}

func init() {
	http.DefaultTransport = NewWasiRoundTripper()
	http.DefaultClient.Transport = NewWasiRoundTripper()
	fetcher.Exports.Fetch = func(url string) FetchResult {
		r, err := fetch(url)
		if err != nil {
			return cm.Err[FetchResult](fmt.Sprintf("fetch: %v", err))
		}
		return cm.OK[FetchResult](r)
	}
}
