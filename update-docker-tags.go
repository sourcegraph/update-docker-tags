package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Masterminds/semver"
)

func main() {
	flag.Usage = func() {
		fmt.Printf(`usage:

Update all image tags in a directory:

  $ cd dir/ && update-docker-tags

Update all image tags in a directory, enforcing constraints:

  $ cd dir/ && update-docker-tags ubuntu=<18.04 alpine=<3.10

		`)
		os.Exit(2)
	}
	flag.Parse()

	// TODO: make this not Sourcegraph-specific
	tagPattern := regexp.MustCompile(`(sourcegraph/.+):(.+)@(sha256:[[:alnum:]]+)`)

	mustNewConstraint := func(c string) *semver.Constraints {
		cs, err := semver.NewConstraint(c)
		if err != nil {
			log.Fatal("cannot parse constraint", err)
		}
		return cs
	}

	constraints := map[string]*semver.Constraints{}
	for _, arg := range flag.Args() {
		split := strings.Split(arg, "=")
		if len(split) != 2 {
			log.Fatal("unexpected argument", arg)
		}
		image, constraint := split[0], split[1]
		constraints[image] = mustNewConstraint(constraint)
	}

	dir := "."
	if err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if info.IsDir() {
			return nil
		}
		if strings.HasPrefix(path, ".git") {
			// Highly doubt anyone would ever want us to traverse git directories.
			return nil
		}

		data, err := ioutil.ReadFile(path)
		if err != nil {
			return err
		}

		printedPath := false
		data = replaceAllSubmatchFunc(tagPattern, data, func(groups [][]byte) [][]byte {
			repository := string(groups[0])

			token, err := fetchAuthToken(repository)
			if err != nil {
				log.Fatal(err)
			}

			tags, err := fetchRepositoryTags(token, repository)
			if err != nil {
				log.Fatal(err)
			}
			var versions []*semver.Version
			for _, tag := range tags {
				v, err := semver.NewVersion(tag)
				if err != nil {
					continue // ignore non-semver tags
				}
				if constraint, ok := constraints[repository]; ok {
					if constraint.Check(v) {
						versions = append(versions, v)
					}
				} else {
					versions = append(versions, v)
				}
			}
			sort.Sort(sort.Reverse(semver.Collection(versions)))

			if len(versions) == 0 {
				fmt.Printf("no semver tags found for %q\n", repository)
				return groups
			}

			newVersion := versions[0].Original()
			newDigest, err := fetchImageDigest(token, repository, newVersion)
			if err != nil {
				log.Fatal(err)
			}
			if !printedPath {
				printedPath = true
				fmt.Println(path)
			}
			fmt.Println("\t", repository, "\t\t", newVersion)
			groups[1] = []byte(newVersion)
			groups[2] = []byte(newDigest)
			return groups
		}, -1)

		if err := ioutil.WriteFile(path, data, info.Mode()); err != nil {
			return err
		}
		return nil
	}); err != nil {
		log.Fatal(err)
	}
}

// Effectively the same as:
//
//  $ curl -s -D - -H "Authorization: Bearer $token" -H "Accept: application/vnd.docker.distribution.manifest.v2+json" https://index.docker.io/v2/sourcegraph/server/manifests/3.12.1 | grep Docker-Content-Digest
//
func fetchImageDigest(token, repository, tag string) (string, error) {
	req, err := http.NewRequest("GET", "https://index.docker.io/v2/"+repository+"/manifests/"+tag, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.docker.distribution.manifest.v2+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	return resp.Header.Get("Docker-Content-Digest"), nil
}

// Effectively the same as:
//
// 	$ export token=$(curl -s "https://auth.docker.io/token?service=registry.docker.io&scope=repository:sourcegraph/server:pull" | jq -r .token)
//
func fetchAuthToken(repository string) (string, error) {
	resp, err := http.Get(fmt.Sprintf("https://auth.docker.io/token?service=registry.docker.io&scope=repository:%s:pull", repository))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	result := struct {
		Token string
	}{}
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		return "", err
	}
	return result.Token, nil
}

// Effectively the same as:
//
// 	$ curl -H "Authorization: Bearer $token" https://index.docker.io/v2/sourcegraph/server/tags/list
//
func fetchRepositoryTags(token, repository string) ([]string, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf("https://index.docker.io/v2/%s/tags/list", repository), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	result := struct {
		Tags []string
	}{}
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		return nil, err
	}
	return result.Tags, nil
}

// replaceAllSubmatchFunc is the missing regexp.ReplaceAllSubmatchFunc; to use it:
//
// 	pattern := regexp.MustCompile(...)
// 	data = replaceAllSubmatchFunc(pattern, data, func(groups [][]byte) [][]byte {
// 		// mutate groups here
// 		return groups
// 	})
//
// This snippet is MIT licensed. Please cite by leaving this comment in place. Find
// the latest version at:
//
//  https://gist.github.com/slimsag/14c66b88633bd52b7fa710349e4c6749
//
func replaceAllSubmatchFunc(re *regexp.Regexp, src []byte, repl func([][]byte) [][]byte, n int) []byte {
	var (
		result  = make([]byte, 0, len(src))
		matches = re.FindAllSubmatchIndex(src, n)
		last    = 0
	)
	for _, match := range matches {
		// Append bytes between our last match and this one (i.e. non-matched bytes).
		matchStart := match[0]
		matchEnd := match[1]
		result = append(result, src[last:matchStart]...)
		last = matchEnd

		// Determine the groups / submatch bytes and indices.
		groups := [][]byte{}
		groupIndices := [][2]int{}
		for i := 2; i < len(match); i += 2 {
			start := match[i]
			end := match[i+1]
			groups = append(groups, src[start:end])
			groupIndices = append(groupIndices, [2]int{start, end})
		}

		// Replace the groups as desired.
		groups = repl(groups)

		// Append match data.
		lastGroup := matchStart
		for i, newValue := range groups {
			// Append bytes between our last group match and this one (i.e. non-group-matched bytes)
			groupStart := groupIndices[i][0]
			groupEnd := groupIndices[i][1]
			result = append(result, src[lastGroup:groupStart]...)
			lastGroup = groupEnd

			// Append the new group value.
			result = append(result, newValue...)
		}
		result = append(result, src[lastGroup:matchEnd]...) // remaining
	}
	result = append(result, src[last:]...) // remaining
	return result
}
