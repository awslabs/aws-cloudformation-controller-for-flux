//go:build integration

// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

package integtests

import (
	"context"
	"fmt"
	"os"

	git "github.com/go-git/go-git/v5"
	http "github.com/go-git/go-git/v5/plumbing/transport/http"
)

const (
	CodeCommitRegion = "us-west-2"
)

// Clones a repository from CodeCommit.
// Returns the temporary directory where the git repository is located
func cloneGitRepository(ctx context.Context, repositoryName string, gitCredentials *http.BasicAuth) (*git.Repository, string, error) {
	tmpDir, err := os.MkdirTemp("", repositoryName)
	if err != nil {
		return nil, tmpDir, fmt.Errorf("unable to create temp dir for repository %s, error: %w", repositoryName, err)
	}

	repositoryUrl := fmt.Sprintf("https://git-codecommit.%s.amazonaws.com/v1/repos/%s", CodeCommitRegion, repositoryName)
	r, err := git.PlainClone(tmpDir, false, &git.CloneOptions{
		URL:  repositoryUrl,
		Auth: gitCredentials,
	})
	if err != nil {
		return r, tmpDir, err
	}
	return r, tmpDir, nil
}

// Copies the given file into the git repository.
// If the destination file name is not specified, the method will create a file using a new unique file name
func copyFileToGitRepository(dir string, repo *git.Repository, gitCredentials *http.BasicAuth, originalFile string, destFile string) (string, error) {
	content, err := os.ReadFile("../../" + originalFile)
	if err != nil {
		return "", err
	}
	return addFileToGitRepository(dir, repo, gitCredentials, string(content), destFile)
}

// Adds a file with the given content into the git repository.
// If the destination file name is not specified, the method will create a file using a new unique file name
func addFileToGitRepository(dir string, repo *git.Repository, gitCredentials *http.BasicAuth, content string, destFile string) (string, error) {
	// Make sure the local repo is up to date with the remote
	w, err := repo.Worktree()
	if err != nil {
		return "", err
	}
	if err = w.Pull(&git.PullOptions{RemoteName: "origin", Auth: gitCredentials}); err != nil && err != git.NoErrAlreadyUpToDate {
		return "", err
	}

	// Write the file into the git repo on disk
	var newFile *os.File
	if destFile == "" {
		newFile, err = os.CreateTemp(dir, "integ-test.*.yaml")
		if err != nil {
			return "", err
		}
	} else {
		newFile, err = os.OpenFile(destFile, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
		if err != nil {
			return "", err
		}
	}
	newFilePath := newFile.Name()
	if _, err = newFile.Write([]byte(content)); err != nil {
		return newFilePath, err
	}
	if err = newFile.Close(); err != nil {
		return newFilePath, err
	}

	// Add the file to git
	if err = w.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		return newFilePath, err
	}
	if _, err = w.Commit("Add file for integ test", &git.CommitOptions{AllowEmptyCommits: false}); err != nil {
		if err == git.ErrEmptyCommit {
			// Nothing to do
			return newFilePath, nil
		}
		return newFilePath, err
	}
	if err = repo.Push(&git.PushOptions{RemoteName: "origin", Auth: gitCredentials}); err != nil {
		return newFilePath, err
	}
	return newFilePath, nil
}

// Deletes files from the git repository
func deleteFilesFromGitRepository(dir string, repo *git.Repository, gitCredentials *http.BasicAuth, filePaths ...string) error {
	// Make sure the local repo is up to date with the remote
	w, err := repo.Worktree()
	if err != nil {
		return err
	}
	if err = w.Pull(&git.PullOptions{RemoteName: "origin", Auth: gitCredentials}); err != nil && err != git.NoErrAlreadyUpToDate {
		return err
	}

	// Delete the files from disk
	for _, filePath := range filePaths {
		if filePath != "" {
			if err = os.Remove(filePath); err != nil {
				return err
			}
		}
	}
	if _, err = w.Commit("Delete integ test files", &git.CommitOptions{All: true, AllowEmptyCommits: false}); err != nil {
		if err == git.ErrEmptyCommit {
			// Nothing to do
			return nil
		}
		return err
	}

	// Delete the files on the remote
	if err = repo.Push(&git.PushOptions{RemoteName: "origin", Auth: gitCredentials}); err != nil {
		return err
	}
	return nil
}
