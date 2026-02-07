// Copyright 2017 The go-github AUTHORS. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The simple command demonstrates a simple functionality which
// prompts the user for a GitHub username and lists all the public
// organization memberships of the specified username.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/google/go-github/v82/github"
)

type PRInfo struct {
	PrNumber              int              `json:"pr_number"`
	PRType                string           `json:"pr_type,omitempty"`
	Title                 string           `json:"title"`
	Author                string           `json:"author"`
	Commits               int              `json:"number_commits,omitempty"`
	Comments              int              `json:"number_comments,omitempty"`
	FileList              []string         `json:"filelist"`
	CreateDate            github.Timestamp `json:"create_date"`
	ChangelogFileContents string           `json:"changelog,omitempty"`
}

// processPullRequest gathers data for one Pull Request
func processPullRequest(ctx context.Context, client *github.Client,
	owner string, repo string, pr *github.PullRequest,
) (PRInfo, error) {
	prNumber := pr.GetNumber()
	title := pr.GetTitle()
	author := pr.GetUser().GetLogin()
	createDate := pr.GetCreatedAt()

	currentPR := PRInfo{
		PrNumber:   prNumber,
		Title:      title,
		Author:     author,
		CreateDate: createDate,
	}

	allFiles, err := getAllPRFiles(client, "restic", "restic", prNumber)
	if err != nil {
		fmt.Fprintf(os.Stderr, "getAllPRFiles failed with %v", err)
		return currentPR, err
	}

	unreleasedFile := ""
	filenames := []string{}
	for _, githubfile := range allFiles {
		filename := githubfile.GetFilename()
		filenames = append(filenames, filename)
		if strings.Contains(filename, "changelog/unreleased/") {
			unreleasedFile = *(githubfile.Patch)
		}
	}
	currentPR.FileList = filenames

	if unreleasedFile != "" {
		lines := strings.Split(unreleasedFile, "\n")
		if len(lines) > 1 {
			lines = lines[1:]
		}
		//
		prTypeLine := lines[0]
		colon := strings.Index(prTypeLine, ":")
		if colon > 1 {
			currentPR.PRType = prTypeLine[1:colon]
		}
		currentPR.ChangelogFileContents = strings.Join(lines, "\n")
	}

	// count Commits
	optList := &github.ListOptions{PerPage: 100}
	repoCommits, _, err := client.PullRequests.ListCommits(ctx, "restic", "restic", prNumber, optList)
	if err != nil {
		fmt.Fprintf(os.Stderr, "listCommits would not run - reason %v\n", err)
		return currentPR, err
	}
	currentPR.Commits = len(repoCommits)

	// count Comments
	optComment := &github.PullRequestListCommentsOptions{ListOptions: github.ListOptions{PerPage: 100}}
	repoComments, _, err := client.PullRequests.ListComments(ctx, "restic", "restic", prNumber, optComment)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ListComments would not run - reason %v\n", err)
		return currentPR, err
	}
	currentPR.Comments = len(repoComments)

	return currentPR, nil
}

func main() {
	ctx := context.Background()

	// 1. Initialize Client
	// Use github.NewClient(nil).WithAuthToken("your_token") for authenticated
	token := os.Getenv("GITHUB_TOKEN") // Retrieved from storage
	client := github.NewClient(nil).WithAuthToken(token)
	prInfos := []PRInfo{}

	// 2. Define Options
	opts := &github.PullRequestListOptions{
		State: "open", // Options: "open", "closed", "all"
		ListOptions: github.ListOptions{
			PerPage: 100, // Max is 100
		},
	}

	// 3. Loop through pages
	counter := 0
	limitCounter := 9999  // limit for testing
	for {
		prs, resp, err := client.PullRequests.List(ctx, "restic", "restic", opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error fetching PRs: %v", err)
			return
		}
		if resp.Rate.Remaining < 10 {
			fmt.Fprintf(os.Stderr, "\nSTOP!\n")
			return
		}

		// prs is a []*PullRequestsService
		for _, pr := range prs {
			prNumber := pr.GetNumber()
			fmt.Fprintf(os.Stderr, "PR %4d Rate Limit: %d/%d\n", prNumber, resp.Rate.Remaining, resp.Rate.Limit)
			currentPR, err := processPullRequest(ctx, client, "restic", "restic", pr)
			if err != nil {
				return
			}
			prInfos = append(prInfos, currentPR)

			counter++
			if counter >= limitCounter {
				break
			}
		}
		if resp.NextPage == 0 || counter >= limitCounter {
			break
		}

		// Update the page for the next iteration
		opts.Page = resp.NextPage
	}

	err := json.NewEncoder(os.Stdout).Encode(prInfos)
	if err != nil {
		fmt.Fprintf(os.Stderr, "can't encode `prInfos` into json - reason %v\n", err)
	}
}

func getAllPRFiles(client *github.Client, owner, repo string, prNumber int) ([]*github.CommitFile, error) {
	var allFiles []*github.CommitFile
	opts := &github.ListOptions{PerPage: 100}

	for {
		files, resp, err := client.PullRequests.ListFiles(context.Background(), owner, repo, prNumber, opts)
		if err != nil {
			return nil, err
		}
		allFiles = append(allFiles, files...)

		// Break the loop if there are no more pages
		if resp.NextPage == 0 {
			break
		}
		// Update the page number for the next request
		opts.Page = resp.NextPage
	}

	return allFiles, nil
}
