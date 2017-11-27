// Testing file for lockmanager

package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"
)

func servetest() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(apiV1))
}

func buildURL(req string, t time.Time, key string) string {
	resource := lockKey(t)

	if key == "" {
		return filepath.Join(req, resource)
	} else {
		return filepath.Join(req, resource, key)
	}
}

func getURL(t *testing.T, srv *httptest.Server, path string, sc int) *http.Response {
	u, _ := url.Parse(srv.URL)
	u.Path = path

	res, err := http.Get(u.String())
	if err != nil {
		t.Log(err)
		t.FailNow()
	}

	if res.StatusCode != sc {
		t.Logf("Status Code Unexpected %v::%v", res.StatusCode, sc)
		t.FailNow()
	}

	return res
}

func checkKey(t *testing.T, res *http.Response) string {
	key := res.Header.Get("AuthKey")

	if len(key) != 32 { // Key is 32 bytes long
		t.Logf("Incorrect key")
		t.FailNow()
	}

	return key
}

func TestRequest(t *testing.T) {
	srv := servetest()
	defer srv.Close()

	u := buildURL(APIV1REQUESTLOCK, time.Now(), "")
	res := getURL(t, srv, u, 200)
	checkKey(t, res)

	// We repeat the request, and we expect the 403 status code
	getURL(t, srv, u, 423)

	time.Sleep(TIMEOUT) // We sleep enough time to free the lock on the resource

	// Repeat Again, now it should grant again the resource
	getURL(t, srv, u, 200)
}

func TestRelease(t *testing.T) {
	srv := servetest()
	defer srv.Close()

	now := time.Now()

	// Create the lock
	u := buildURL(APIV1REQUESTLOCK, now, "")
	res := getURL(t, srv, u, 200)
	key := checkKey(t, res)

	// We release now the lock
	// Check Unauthorized
	u = buildURL(APIV1RELEASELOCK, now, "unauthorized")
	getURL(t, srv, u, 401)

	// Check Release
	u = buildURL(APIV1RELEASELOCK, now, key)
	getURL(t, srv, u, 200)

	// Check NotFound
	u = buildURL(APIV1RELEASELOCK, now, key)
	getURL(t, srv, u, 404)
}

func TestRefresh(t *testing.T) {
	srv := servetest()
	defer srv.Close()

	now := time.Now()

	// Create the lock
	u := buildURL(APIV1REQUESTLOCK, now, "")
	res := getURL(t, srv, u, 200)
	key := checkKey(t, res)

	// Check Refresh
	ref := buildURL(APIV1REFRESHLOCK, now, key)
	getURL(t, srv, ref, 200)

	// Check Unauthorized
	u = buildURL(APIV1REFRESHLOCK, now, "unauthorized")
	getURL(t, srv, u, 401)

	// Check NotFound
	u = buildURL(APIV1REFRESHLOCK, time.Unix(0, 0), key)
	getURL(t, srv, u, 404)

	// Check Lock Released
	rel := buildURL(APIV1RELEASELOCK, now, key)
	getURL(t, srv, rel, 200)

	time.Sleep(TIMEOUT)
	getURL(t, srv, ref, 410) // We try to refresh now a Released resource
}
