// Copyright 2016 The Cockroach Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
// implied. See the License for the specific language governing
// permissions and limitations under the License.
//
// Author: Tamir Duberstein (tamird@gmail.com)

package main

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"testing"

	"github.com/google/go-github/github"
)

func TestRunGH(t *testing.T) {
	f, err := os.Open("testdata/stress-failure")
	if err != nil {
		t.Fatal(err)
	}

	const (
		expOwner  = "cockroachdb"
		expRepo   = "cockroach"
		pkg       = "foo/bar/baz"
		sha       = "abcd123"
		serverURL = "teamcity.example.com"
		buildID   = 8008135
		issueID   = 1337
	)

	issueBodyRe := regexp.MustCompile(
		fmt.Sprintf(`(?s)\ASHA: https://github.com/cockroachdb/cockroach/commits/%s

Stress build found a failed test: %s

.*
%s
`,
			regexp.QuoteMeta(sha),
			regexp.QuoteMeta(fmt.Sprintf("https://%s/viewLog.html?buildId=%d", serverURL, buildID)),
			regexp.QuoteMeta("	<autogenerated>:12: storage/replicate_queue_test.go:103, condition failed to evaluate within 45s: not balanced: [10 1 10 1 8]"),
		),
	)

	for key, value := range map[string]string{
		teamcityVCSNumberEnv: sha,
		teamcityServerURLEnv: serverURL,
		teamcityBuildIDEnv:   strconv.Itoa(buildID),
	} {
		if val, ok := os.LookupEnv(key); ok {
			defer func() {
				if err := os.Setenv(key, val); err != nil {
					t.Error(err)
				}
			}()
		} else {
			defer func() {
				if err := os.Unsetenv(key); err != nil {
					t.Error(err)
				}
			}()
		}

		if err := os.Setenv(key, value); err != nil {
			t.Fatal(err)
		}
	}

	count := 0
	if err := runGH(
		f,
		func(owner string, repo string, issue *github.IssueRequest) (*github.Issue, *github.Response, error) {
			count++
			if owner != expOwner {
				t.Fatalf("got %s, expected %s", owner, expOwner)
			}
			if repo != expRepo {
				t.Fatalf("got %s, expected %s", repo, expRepo)
			}
			// TODO(tamird): why is the package name blank?
			_ = pkg // suppress unused warning
			if expected := fmt.Sprintf("%s: %s failed under stress", "", "TestReplicateQueueRebalance"); *issue.Title != expected {
				t.Fatalf("got %s, expected %s", *issue.Title, expected)
			}
			if !issueBodyRe.MatchString(*issue.Body) {
				t.Fatalf("got:\n%s\nexpected:\n%s", *issue.Body, issueBodyRe)
			}
			if length := len(*issue.Body); length > githubIssueBodyMaximumLength {
				t.Fatalf("issue length %d exceeds (undocumented) maximum %d", length, githubIssueBodyMaximumLength)
			}
			return &github.Issue{ID: github.Int(issueID)}, nil, nil
		},
	); err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Fatal("no issue was posted")
	}
}
