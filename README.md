bonjour
====

This is a simple Multicast DNS-SD (Apple Bonjour) library written in Golang. You can use it to discover services in the LAN. Pay attention to the infrastructure you are planning to use it (clouds or shared infrastructures usually prevent mDNS from functioning). But it should work in the most office, home and private environments.

It does NOT pretend to be a full & valid implementation of the RFC 6762 & RFC 6763, but it fulfils the requirements of its authors (we just needed service discovery in the LAN environment for our IoT products).


##Browsing available services in your local network

Here is an example how to browse services by their type:

```
package main

import (
    "log"

    "github.com/oleksandr/bonjour"
)

func main() {
    // Channel for results
    results := make(chan *bonjour.ServiceEntry)

    // Results handling goroutine
    go func(results chan *bonjour.ServiceEntry) {
        for e := range results {
            log.Printf("%#v", e)
        }
    }(results)

    // Start a browser (blocking call)
    err := bonjour.Browse("_foobar._tcp", "", results, nil)
    if err != nil {
        log.Println(err.Error())
    }
}
```

##Doing a lookup of a specific service instance

Here is an example of looking up service by service instance name:

```
package main

import (
    "log"

    "github.com/oleksandr/bonjour"
)

func main() {
    // Channel for results
    results := make(chan *bonjour.ServiceEntry)

    // Results handling goroutine
    go func(results chan *bonjour.ServiceEntry) {
        for e := range results {
            log.Printf("%#v", e)
        }
    }(results)

    // Start a lookup (blocking call)
    err := bonjour.Lookup("Demo", "_foobar._tcp", "", results, nil)
    if err != nil {
        log.Println(err.Error())
    }
}
```


##Registering a service

Work in progress...
