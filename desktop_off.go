//go:build !desktop

package main

func runNoArgs() int {
	usage()
	return 2
}
