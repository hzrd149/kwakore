//go:build !linux

package osintegration

func StartSearchProvider(func(query string)) func() { return func() {} }
