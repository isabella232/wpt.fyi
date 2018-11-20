// Copyright 2017 The WPT Dashboard Project. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package shared

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	mapset "github.com/deckarep/golang-set"
	"golang.org/x/net/context"
	"google.golang.org/appengine/urlfetch"
)

// FetchRunResultsJSONForParam fetches the results JSON blob for the given [product]@[SHA] param.
func FetchRunResultsJSONForParam(
	ctx context.Context, r *http.Request, param string) (results map[string][]int, err error) {
	afterDecoded, err := base64.URLEncoding.DecodeString(param)
	if err == nil {
		var run TestRun
		if err = json.Unmarshal([]byte(afterDecoded), &run); err != nil {
			return nil, err
		}
		return FetchRunResultsJSON(ctx, r, run)
	}
	var spec ProductSpec
	if spec, err = ParseProductSpec(param); err != nil {
		return nil, err
	}
	return FetchRunResultsJSONForSpec(ctx, r, spec)
}

// FetchRunResultsJSONForSpec fetches the result JSON blob for the given spec.
func FetchRunResultsJSONForSpec(
	ctx context.Context, r *http.Request, spec ProductSpec) (results map[string][]int, err error) {
	var run *TestRun
	if run, err = FetchRunForSpec(ctx, spec); err != nil {
		return nil, err
	} else if run == nil {
		return nil, nil
	}
	return FetchRunResultsJSON(ctx, r, *run)
}

// FetchRunForSpec loads the wpt.fyi TestRun metadata for the given spec.
func FetchRunForSpec(ctx context.Context, spec ProductSpec) (*TestRun, error) {
	one := 1
	testRuns, err := LoadTestRuns(ctx, []ProductSpec{spec}, nil, spec.Revision, nil, nil, &one, nil)
	if err != nil {
		return nil, err
	}
	if len(testRuns) == 1 {
		for _, v := range testRuns {
			if len(v) == 1 {
				return &v[0], nil
			}
		}
	}
	return nil, nil
}

// FetchRunResultsJSON fetches the results JSON summary for the given test run, but does not include subtests (since
// a full run can span 20k files).
func FetchRunResultsJSON(ctx context.Context, r *http.Request, run TestRun) (results map[string][]int, err error) {
	client := urlfetch.Client(ctx)
	url := strings.TrimSpace(run.ResultsURL)
	if strings.Index(url, "/") == 0 {
		reqURL := *r.URL
		reqURL.Path = url
	}
	var resp *http.Response
	if resp, err = client.Get(url); err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var body []byte
	if body, err = ioutil.ReadAll(resp.Body); err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s returned HTTP status %d:\n%s", url, resp.StatusCode, string(body))
	}
	if err = json.Unmarshal(body, &results); err != nil {
		return nil, err
	}
	return results, nil
}

// GetResultsDiff returns a map of test name to an array of [newly-passing, newly-failing, total-delta], for tests which had
// different results counts in their map (which is test name to array of [count-passed, total]).
//
func GetResultsDiff(
	before map[string][]int,
	after map[string][]int,
	filter DiffFilterParam,
	paths mapset.Set,
	renames map[string]string) map[string][]int {
	diff := make(map[string][]int)
	if filter.Deleted || filter.Changed {
		for test, resultsBefore := range before {
			if renames != nil {
				rename, ok := renames[test]
				if ok {
					test = rename
				}
			}
			if !anyPathMatches(paths, test) {
				continue
			}

			if resultsAfter, ok := after[test]; !ok {
				// NOTE(lukebjerring): Missing tests are only counted towards changes
				// in the total.
				if !filter.Deleted {
					continue
				}
				diff[test] = []int{0, 0, -resultsBefore[1]}
			} else {
				if !filter.Changed && !filter.Unchanged {
					continue
				}
				delta := resultsBefore[0] - resultsAfter[0]
				changed := delta != 0 || resultsBefore[1] != resultsAfter[1]
				if (!changed && !filter.Unchanged) || changed && !filter.Changed {
					continue
				}

				improved, regressed := 0, 0
				if d := resultsAfter[0] - resultsBefore[0]; d > 0 {
					improved = d
				}
				failingBefore := resultsBefore[1] - resultsBefore[0]
				failingAfter := resultsAfter[1] - resultsAfter[0]
				if d := failingAfter - failingBefore; d > 0 {
					regressed = d
				}
				// Changed tests is at most the number of different outcomes,
				// but newly introduced tests should still be counted (e.g. 0/2 => 0/5)
				diff[test] = []int{
					improved,
					regressed,
					resultsAfter[1] - resultsBefore[1],
				}
			}
		}
	}
	if filter.Added {
		for test, resultsAfter := range after {
			// Skip 'added' results of a renamed file (handled above).
			if renames != nil {
				renamed := false
				for _, is := range renames {
					if is == test {
						renamed = true
						break
					}
				}
				if renamed {
					continue
				}
			}
			if !anyPathMatches(paths, test) {
				continue
			}

			if _, ok := before[test]; !ok {
				// Missing? Then N / N tests are 'different'
				diff[test] = []int{resultsAfter[0], resultsAfter[1] - resultsAfter[0], resultsAfter[1]}
			}
		}
	}
	return diff
}

func anyPathMatches(paths mapset.Set, testPath string) bool {
	if paths == nil {
		return true
	}
	for path := range paths.Iter() {
		if strings.Index(testPath, path.(string)) == 0 {
			return true
		}
	}
	return false
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func max(x int, y int) int {
	if x < y {
		return y
	}
	return x
}
