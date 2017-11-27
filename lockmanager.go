/* This program, is a service that manages arbitrary named resources.
   It does lock, release and refresh resources as requested by its customers.
   It will automatically release a lock on any resource after a time period.
   The lock requester must extend the lease time periodically, otherwise it will lose the lock
   The web server has instructions on any non api request
*/

package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

const DEFAULTWEBPORT = "8080"
const APIV1 = "/api.v1/"
const APIV1REQUESTLOCK = APIV1 + "RequestLock/"
const APIV1RELEASELOCK = APIV1 + "ReleaseLock/"
const APIV1REFRESHLOCK = APIV1 + "RefreshLock/"
const TIMEOUT = 3 * time.Second // Seconds to wait before giving up locked resources

// Concurrent map, I pretended to use channels to sync the map access
// but sync.Map does this better
var locks sync.Map

var (
	ipWeb = flag.String("ip", "localhost:"+DEFAULTWEBPORT, "Web IP:PORT used to listen ie: *:8081, :8081, localhost")
	help  = flag.Bool("help", false, "Print Usage options")
)

func Usage() {
	fmt.Println("Usage: ", os.Args[0], "[-ip, -help]")
	fmt.Println("   ie: ", os.Args[0], "-ip *:8081")
	os.Exit(2)
}

func signals() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGQUIT) // Kill signal not needed as its handled by the OS
	sig := <-sigCh
	log.Printf("Signal received %v\n", sig)
	fmt.Fprintf(os.Stderr, "Signal received %v\n", sig)
	os.Exit(1)
}

// Deals with Serving web content, static or websockets
func documentation(w http.ResponseWriter, r *http.Request) {
	log.Printf("Documentation from [%s]\n", r.URL)
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		log.Printf("  Method not allowed [%s]\n", r.Method)
		return
	}

	http.ServeFile(w, r, "./documentation.html")
}

// Default Api entry call
func apiV1(w http.ResponseWriter, r *http.Request) {

	url := r.URL.Path

	switch {
	case strings.HasPrefix(url, APIV1REQUESTLOCK):
		apiV1RequestLock(w, r)
	case strings.HasPrefix(url, APIV1RELEASELOCK):
		apiV1ReleaseLock(w, r)
	case strings.HasPrefix(url, APIV1REFRESHLOCK):
		apiV1RefreshLock(w, r)
	default:
		log.Printf("API Call %s from %s\n", r.URL.Path, getIP(r.RemoteAddr))
		http.Error(w, "Api Call Not Found", http.StatusNotFound)
	}
}

// Returns the resource name and the auth Key from a URL
func retrieveKey(path string) (string, string, error) {
	components := strings.Split(path, "/")
	if len(components) != 2 {
		return "", "", errors.New("Partial content")
	}

	resource, key := components[0], components[1]

	if resource == "" || key == "" {
		return "", "", errors.New("Empty content")
	}
	return resource, key, nil
}

// Api V1 Calls

// RequestLock, takes a named resource and returns an authentication key if available
func apiV1RequestLock(w http.ResponseWriter, r *http.Request) {
	log.Printf("Call %s from %s\n", r.URL, getIP(r.RemoteAddr))

	resource := r.URL.Path[len(APIV1REQUESTLOCK):] // Removes the /api.v1/RequestLock/ part

	// Checks resource exists and is alone in the path
	if resource == "" || strings.Contains(resource, "/") {
		log.Printf("apiV1RequestLock %v  %s\n", http.StatusBadRequest, resource)
		http.Error(w, "Api Bad Request", http.StatusBadRequest)
		return
	}

	// Does resource already exists?
	li, ok := locks.Load(resource)

	if ok { // resource lock existed
		linfo := li.(*lockInfo) // Recovered lockInfo from Database

		isLocked := linfo.tryLockTimeout(TIMEOUT)

		if isLocked { // Resource exists and we got a lock on it
			log.Println("Get request lock ")

			// Verify lease is still being used, otherwise we can reuse it
			if linfo.isActive() && linfo.isOnTime(TIMEOUT) {
				log.Printf("apiV1RequestLock %v  %s\n", http.StatusLocked, resource)
				http.Error(w, "Resource already in use", http.StatusLocked)
				linfo.unLock()
				return
			}
		} else { // Resource exists and we did not got a lock on it (Timeout)
			log.Printf("apiV1RequestLock %v  %s\n", http.StatusRequestTimeout, resource)
			http.Error(w, "Resource already in use", http.StatusRequestTimeout)
			return
		}

		// Here the lockInfo is not longer active nor used, so we disable it
		linfo.disable()
	}

	var lockReq *lockInfo = newLockInfo(r)

	// Assign Lock on Resource, We arrive here by lock creation or lock appropiation
	locks.Store(resource, lockReq)

	// Return the Success Code and the AuthKey for the lock
	w.Header().Set("AuthKey", lockReq.key())
	fmt.Fprintln(w, "Lock Granted")
	log.Printf("apiV1RequestLock %v <%s/%v>\n", http.StatusOK, resource, lockReq.key())
}

// ReleaseLock, takes a name resource and a key and releases the lock if it exists
func apiV1ReleaseLock(w http.ResponseWriter, r *http.Request) {
	log.Printf("Call %s from %s\n", r.URL, getIP(r.RemoteAddr))

	resource, key, err := retrieveKey(r.URL.Path[len(APIV1REFRESHLOCK):])
	if err != nil {
		log.Printf("apiV1ReleaseLock %v  %s\n", http.StatusBadRequest, resource)
		http.Error(w, "Api Bad Request", http.StatusBadRequest)
		return
	}

	// Search resource lockInfo
	li, ok := locks.Load(resource)
	if !ok {
		log.Printf("apiV1ReleaseLock %v  %s\n", http.StatusNotFound, resource)
		http.Error(w, "Resource not Found", http.StatusNotFound)
		return
	}

	linfo := li.(*lockInfo)
	linfo.lock() // We want the lock, not matter how much time it must wait.
	defer linfo.unLock()

	if !linfo.isActive() {
		log.Printf("apiV1ReleaseLock %v  %s\n", http.StatusNotFound, resource)
		http.Error(w, "Resource not Found", http.StatusNotFound)
		return
	}

	if linfo.isRequestAuthorithed(r, key) {
		log.Printf("apiV1ReleaseLock %v  %s\n", http.StatusUnauthorized, resource)
		http.Error(w, "Not authorized", http.StatusUnauthorized)
		return
	}

	// Wait until the lease expires, so any refresh handler for this resource
	// will be informed that the resource is not longer his own
	time.Sleep(linfo.expiryTime())

	// Release the resource, We don't fully erase the lock as we want to inform
	// possible refresh handlers that the resource lock has been released, but
	// with very demanded resources this can become a not found.
	linfo.disable()
	fmt.Fprintln(w, "Lock Released")
	log.Printf("apiV1ReleaseLock %v  %s\n", http.StatusOK, resource)
}

// RefreshLock, takes a name resource and a key and grants another time period of exclusive usage
func apiV1RefreshLock(w http.ResponseWriter, r *http.Request) {
	log.Printf("Call %s from %s\n", r.URL, getIP(r.RemoteAddr))

	resource, key, err := retrieveKey(r.URL.Path[len(APIV1REFRESHLOCK):])
	if err != nil {
		log.Printf("apiV1RefreshLock <%s:%s> :: %v\n", resource, key, http.StatusBadRequest)
		http.Error(w, "Api Bad Request", http.StatusBadRequest)
		return
	}

	// Search resource lockInfo
	li, ok := locks.Load(resource)
	if !ok {
		log.Printf("apiV1RefreshLock <%s:%s> :: %v\n", resource, key, http.StatusNotFound)
		http.Error(w, "Resource not Found", http.StatusNotFound)
		return
	}

	linfo := li.(*lockInfo)
	isLocked := linfo.tryLockTimeout(TIMEOUT)

	if isLocked {
		defer linfo.unLock()

		if !linfo.isActive() {
			log.Printf("apiV1RefreshLock <%s:%s> :: %v\n", resource, key, http.StatusGone)
			http.Error(w, "Lock expired and released", http.StatusGone)
			return
		}

		if linfo.isRequestAuthorithed(r, key) {
			log.Printf("apiV1RefreshLock <%s:%s> :: %v\n", resource, key, http.StatusUnauthorized)
			http.Error(w, "Not authorized", http.StatusUnauthorized)
			return
		}

		// Everything is right, Update refresh time on Lock
		linfo.refresh()
		fmt.Fprintln(w, "Lock Refreshed")
		log.Printf("apiV1RefreshLock %v  %s\n", http.StatusOK, resource)
	} else {
		// Not able to lock
		log.Printf("apiV1RefreshLock %v  %s\n", http.StatusLocked, resource)
		http.Error(w, "Resource locked, Unable to refresh", http.StatusLocked)
		return
	}
}

func serve(ip string) {
	log.Println("http Server on ", ip)
	http.HandleFunc("/", documentation)
	http.HandleFunc("/api.v1/", apiV1)
	log.Fatalf("Error: ", http.ListenAndServe(ip, nil))
}

func main() {
	log.Println("Main")
	flag.Usage = Usage
	flag.Parse()

	if len(flag.Args()) > 0 || *help {
		Usage()
	}

	go signals()

	*ipWeb = strings.Replace(*ipWeb, "*", "0.0.0.0", 1) // "*" are not processed by ListenAndServe function
	serve(*ipWeb)
}
