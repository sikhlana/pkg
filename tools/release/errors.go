package main

import "errors"

var (
	errCheckFailed = errors.New("release check failed")
	errDirtyTree   = errors.New("the working tree has uncommitted changes to tracked files")
	errJobs        = errors.New("--jobs must be at least 1")
	errNoToken     = errors.New("GITHUB_TOKEN is empty; set it, or pass --dry-run")
)
