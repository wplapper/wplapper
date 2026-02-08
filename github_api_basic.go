// Copyright 2017 The go-github AUTHORS. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The simple command demonstrates a simple functionality which
// prompts the user for a GitHub username and lists all the public
// organization memberships of the specified username.
// https://pkg.go.dev/github.com/google/go-github/v82/github

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/google/go-github/v82/github"
)

type CommitFile struct {
	Filename    string `json:"filename"`
	Additions   int    `json:"additions"`
	Deletions   int    `json:"deletions"`
	LinesInFile int    `json:"lines_in_file"`
	FileSize    int    `json:"filesize"`
}

type PRInfo struct {
	PrNumber              int              `json:"pr_number"`
	PRType                string           `json:"pr_type,omitempty"`
	Title                 string           `json:"title"`
	Author                string           `json:"author"`
	Commits               int              `json:"number_commits,omitempty"`
	Comments              int              `json:"number_comments,omitempty"`
	CreateDate            github.Timestamp `json:"create_date"`
	ChangelogFileContents string           `json:"changelog,omitempty"`
	Additions             int              `json:"additions"`
	Deletions             int              `json:"deletions"`
	LinesInFile           int              `json:"lines_in_file"`
	FileSize              int              `json:"filesize"`
	FileList              []CommitFile     `json:"filelist"`
}

func getFileContentLength(ctx context.Context, client *github.Client, owner string,
	repo string, path string, ref string,
) (int, int, error) {
	fileContent, _, _, err := client.Repositories.GetContents(ctx, owner, repo, path,
		&github.RepositoryContentGetOptions{Ref: ref})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Repositories.GetContents failed with %v\n", err)
		return 0, 0, err
	}

	content, err := fileContent.GetContent()
	if err != nil {
		fmt.Fprintf(os.Stderr, "GetContent failed with %v\n", err)
		return 0, 0, err
	}

	// Count the lines
	lineCount := strings.Count(content, "\n")

	// If the file isn't empty and doesn't end in a newline, add 1
	if len(content) > 0 && !strings.HasSuffix(content, "\n") {
		lineCount++
	}

	return lineCount, len(content), nil
}

// processPullRequest gathers the data for one Pull Request
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

	head := pr.GetHead()
	headOwner := head.GetRepo().GetOwner().GetLogin()
	headRepo := head.GetRepo().GetName()
	headSHA := head.GetSHA()
	unreleasedFile := ""
	commitFiles := []CommitFile{}
	additions := 0
	deletions := 0
	linesInFile := 0
	fileSize := 0
	for _, githubfile := range allFiles {
		filename := *(githubfile.Filename)
		if strings.Contains(filename, "changelog/unreleased/") {
			unreleasedFile = *(githubfile.Patch)
		}

		linesOfCode, size, err := getFileContentLength(ctx, client, headOwner, headRepo, filename, headSHA)
		if err != nil {
			fmt.Fprintf(os.Stderr, "getFileContentLength failed with %v\n", err)
			err = nil
		}

		currentFile := CommitFile{
			Filename:    filename,
			Additions:   *(githubfile.Additions),
			Deletions:   *(githubfile.Deletions),
			LinesInFile: linesOfCode,
			FileSize:    size,
		}
		additions += *(githubfile.Additions)
		deletions += *(githubfile.Deletions)
		linesInFile += linesOfCode
		fileSize += size
		commitFiles = append(commitFiles, currentFile)
	}
	currentPR.FileList = commitFiles
	currentPR.Additions = additions
	currentPR.Deletions = deletions
	currentPR.LinesInFile = linesInFile
	currentPR.FileSize = fileSize

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
		currentPR.ChangelogFileContents = lines[0][1:]
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
	limitCounter := 999999 // limit for testing
	for {
		prs, resp, err := client.PullRequests.List(ctx, "restic", "restic", opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error fetching PRs: %v\n", err)
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
