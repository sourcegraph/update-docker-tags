package main

import (
	"regexp"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func Test_pattern(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  [][]string
	}{
		{
			name:  "prom",
			input: "FROM quay.io/prometheus/busybox-linux-amd64:latest@sha256:0c38f63cbe19e40123668a48c36466ef72b195e723cbfcbe01e9657a5f14cec6",
			want: [][]string{{
				"quay.io/prometheus/busybox-linux-amd64:latest@sha256:0c38f63cbe19e40123668a48c36466ef72b195e723cbfcbe01e9657a5f14cec6",
				"quay.io/prometheus/busybox-linux-amd64", "latest",
				"sha256:0c38f63cbe19e40123668a48c36466ef72b195e723cbfcbe01e9657a5f14cec6",
			}},
		},
		{
			name:  "prom2",
			input: "FROM prom/prometheus:v2.16.0@sha256:e4ca62c0d62f3e886e684806dfe9d4e0cda60d54986898173c1083856cfda0f4 AS upstream",
			want: [][]string{{
				"prom/prometheus:v2.16.0@sha256:e4ca62c0d62f3e886e684806dfe9d4e0cda60d54986898173c1083856cfda0f4",
				"prom/prometheus", "v2.16.0",
				"sha256:e4ca62c0d62f3e886e684806dfe9d4e0cda60d54986898173c1083856cfda0f4",
			}},
		},
		{
			name:  "golang",
			input: "FROM golang:1.13-alpine@sha256:ed003971a4809c9ae45afe2d318c24b9e3f6b30864a322877c69a46c504d852c AS builder",
			want: [][]string{{
				"golang:1.13-alpine@sha256:ed003971a4809c9ae45afe2d318c24b9e3f6b30864a322877c69a46c504d852c",
				"golang", "1.13-alpine",
				"sha256:ed003971a4809c9ae45afe2d318c24b9e3f6b30864a322877c69a46c504d852c",
			}},
		},
		{
			name:  "dont patch filepaths",
			input: `import("foo/bar")`,
			want:  nil,
		},
	}
	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			got := regexp.MustCompile(defaultPattern).FindAllStringSubmatch(tst.input, -1)
			if diff := cmp.Diff(tst.want, got); diff != "" {
				t.Errorf("%v", diff)
			}
		})
	}
}
