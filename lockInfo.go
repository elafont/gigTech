/* This file deals with lockInfo and its methods
 */

package main

import (
	"crypto/md5"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/vburenin/nsync"
)

// lock structure, stores the status of a lock
type lockInfo struct {
	active    bool
	created   time.Time
	refreshed time.Time
	ip        string
	mutex     *nsync.TryMutex
}

// All lockInfo methods start with a lower case name as this is not a module
// and its not designed to be one

func newLockInfo(r *http.Request) *lockInfo {
	creationDate := time.Now()

	var lockReq *lockInfo = &lockInfo{
		active:    true,
		created:   creationDate,
		refreshed: creationDate,
		ip:        getIP(r.RemoteAddr),
		mutex:     nsync.NewTryMutex(),
	}

	return lockReq
}

// Refreshes the lease time of the resource lock, ie: extends it for another period
func (li *lockInfo) refresh() {
	li.refreshed = time.Now()
}

// Disables the resource lock,
func (li *lockInfo) disable() {
	li.refreshed = time.Unix(0, 0) // Forces lock to be outdated
	li.ip = ""
	li.active = false
}

// Wrapper around nsync.TryMutex
func (li *lockInfo) tryLockTimeout(t time.Duration) bool {
	return li.mutex.TryLockTimeout(t)
}

// Wrapper around nsync.TryMutex
func (li *lockInfo) lock() {
	li.mutex.Lock()
}

// Wrapper around nsync.TryMutex
func (li *lockInfo) unLock() {
	li.mutex.Unlock()
}

// returns the activity status of a resource lock
func (li *lockInfo) isActive() bool {
	return (li.active)
}

// returns the activity status of a resource lock
func (li *lockInfo) isOnTime(t time.Duration) bool {
	return (time.Since(li.refreshed) < t)
}

// returns true if the request has autority over the resource
func (li *lockInfo) isRequestAuthorithed(r *http.Request, key string) bool {
	return (key != li.key() || getIP(r.RemoteAddr) != li.ip)
}

// returns the key of the lock, used to authenticate requests
func (li *lockInfo) key() string {
	return lockKey(li.created)
}

// Lock Info main key function, returns the key associated to a given time
func lockKey(t time.Time) string {
	return fmt.Sprintf("%x", hash(t))
}

// return the time to wait until the lease expires
func (li *lockInfo) expiryTime() time.Duration {
	return li.refreshed.Add(TIMEOUT).Sub(time.Now())
}

// Simple function that drops the port part from an IP address
func getIP(addr string) string {
	return strings.Split(addr, ":")[0]
}

// Returns a simple hash on a date, used to create keys to authenticate requests
func hash(seed time.Time) []byte {
	h := md5.Sum([]byte(seed.String()))
	return h[:]
}
