Notes about this LockManager

1.- It does manage arbitrary resources using a string as a name
2.- It can Lock, Release and Refresh a LockManager
3.- A Refresh must happend every TIMEOUT seconds period, otherwise, other 
    processes trying to get a lock on the same resource will get it.
    This is done to avoid dead processes that does not releases locks
4.- Releasing a lock has precedence over Refresh, ie: if two threads of the 
    same process one refreshes the lock and the other releases the lock, then 
    the release will occur no matter what.
5.- When acquiring a lock, the server return a header with the AuthKey name.
    This header contains the key that must be used to identify the process that
    OWNS the lock in the first place. It also checks for the IP, so to operate
    on a lock you need to be on the same machine that acquired the lock in the
    first place and have the key that allows the request to complete.
        
When you ask for an already locked resource, the program does not block the 
execution, it waits for the lock timeout, if it can access the resource then 
assigns the resource to the new owner, otherwise it returns a locked resource 
message to the requester so the proccess can continue operating and avoid deadlocks.

It forces the owner of the lock to notify regularly, that still wants the lock, 
the program will release automatically the lock if any other process wants the 
resource and the first owner has not refreshed his ownership. The locks have a 
lease time, after this time, the lease became free to other processes to acquire 
it, unless a refresh by the first owner arrives first, then the lease period will
be extended. When a resorce is lost due to avoidance of refreshing the lease 
period, any posterior refresh may return a 404 or 410, depending on the time
from the last refresh, and/or the competition for the resources.

The program seems not very Golang idiomatic, as this problem has a more natural
procedural solution than an Object Oriented one. 

All the "golang magic" is done behind the curtains, sync.Map, vburenin.TryMutex, 
and http.ListenAndServe, hides all goroutines, channels, and synchronizations 
processes occurring in the software.

The first prototype of this challenge was done using channels, and a normal Map
but the code did not look "nice" as it added a lot of complexities that distract
reader from the real intentions of the software. So I rebuild the software using
a more straightforward approach.

This is a simple program, so I used the more simple tools and solutions I imagined,
that's why I used a http server with a REST like API, as communication between 
processes does not need to keep state, it seemed to me, more natural and easy.

Errors a treated in the most simple Golang way possible, not using any other 
pattern to process them as most of the code carry a return just after announcing 
the error. In other applications, I may use error patterns to avoid the long list
of "if err != nil ..." that plagues the code. Anyways it's not bad, it's the golang 
way, as it is the C way too, but some readers get nervous when reading so many "if.."




