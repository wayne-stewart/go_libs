package main

import (
	"fmt"
	"net/http"
)

func PrintRequestLine(r *http.Request) {
	fmt.Printf("%s %s %s\n", r.Method, r.RequestURI, r.Proto)
}

func PrintRequestHeaders(r *http.Request) {
	fmt.Printf("%s %s %s\n", r.Method, r.RequestURI, r.Proto)
	for x := range r.Header {
		fmt.Printf("Header: %s: %s\n", x, r.Header.Get(x))
	}
}
